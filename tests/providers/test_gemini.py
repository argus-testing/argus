import asyncio
import json

import httpx
import pytest

from argus.providers.gemini import GeminiProvider
from argus.runtime.errors import RateLimitedError
from argus.runtime.messages import (
    AudioPart,
    ImagePart,
    Message,
    MessageRole,
    TextPart,
    ToolCallPart,
    ToolResultPart,
)
from argus.runtime.models import GenerationOptions, ModelRef
from argus.runtime.providers import ModelRequest, ModelResponse, TextDelta


def test_gemini_streams_multimodal_request_without_google_sdk() -> None:
    async def handler(request: httpx.Request) -> httpx.Response:
        assert request.headers["x-goog-api-key"] == "test-key"
        assert request.url.path.endswith(
            "/v1beta/models/gemini-3-flash-preview:streamGenerateContent"
        )
        payload = json.loads(request.content)
        assert payload["systemInstruction"] == {
            "parts": [{"text": "You are a visual tester."}]
        }
        assert payload["contents"] == [
            {
                "role": "user",
                "parts": [
                    {"text": "Describe this page."},
                    {
                        "inlineData": {
                            "mimeType": "image/png",
                            "data": "cG5n",
                        }
                    },
                    {
                        "inlineData": {
                            "mimeType": "audio/mpeg",
                            "data": "bXAz",
                        }
                    },
                ],
            }
        ]
        return httpx.Response(
            200,
            text=(
                'data: {"candidates":[{"content":{"parts":[{"text":"Hel"}]}}]}\n\n'
                'data: {"candidates":[{"content":{"parts":[{"text":"lo"}]}}]}\n\n'
            ),
            headers={"content-type": "text/event-stream"},
        )

    async def exercise() -> None:
        async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as client:
            provider = GeminiProvider(api_key="test-key", client=client)
            request = ModelRequest(
                model=ModelRef(provider="gemini", model="gemini-3-flash-preview"),
                messages=(
                    Message(
                        role=MessageRole.SYSTEM,
                        parts=(TextPart(text="You are a visual tester."),),
                    ),
                    Message(
                        role=MessageRole.USER,
                        parts=(
                            TextPart(text="Describe this page."),
                            ImagePart(data=b"png", media_type="image/png"),
                            AudioPart(data=b"mp3", media_type="audio/mpeg"),
                        ),
                    ),
                ),
                tools=(),
                generation=GenerationOptions(),
            )

            events = [event async for event in provider.stream(request)]

        assert events == [
            TextDelta(text="Hel"),
            TextDelta(text="lo"),
            ModelResponse(parts=(TextPart(text="Hello"),)),
        ]

    asyncio.run(exercise())


def test_gemini_echoes_matching_function_call_ids() -> None:
    async def handler(request: httpx.Request) -> httpx.Response:
        payload = json.loads(request.content)
        assert payload["contents"] == [
            {
                "role": "model",
                "parts": [
                    {
                        "functionCall": {
                            "id": "call-1",
                            "name": "read_page",
                            "args": {},
                        }
                    }
                ],
            },
            {
                "role": "user",
                "parts": [
                    {
                        "functionResponse": {
                            "id": "call-1",
                            "name": "read_page",
                            "response": {"text": "Page"},
                        }
                    }
                ],
            },
        ]
        return httpx.Response(
            200,
            text='data: {"candidates":[{"content":{"parts":[{"text":"Done"}]}}]}\n\n',
            headers={"content-type": "text/event-stream"},
        )

    async def exercise() -> None:
        async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as client:
            provider = GeminiProvider(api_key="test-key", client=client)
            events = [
                event
                async for event in provider.stream(
                    ModelRequest(
                        model=ModelRef(
                            provider="gemini", model="gemini-3-flash-preview"
                        ),
                        messages=(
                            Message(
                                role=MessageRole.ASSISTANT,
                                parts=(
                                    ToolCallPart(
                                        call_id="call-1",
                                        name="read_page",
                                        arguments={},
                                    ),
                                ),
                            ),
                            Message(
                                role=MessageRole.TOOL,
                                parts=(
                                    ToolResultPart(
                                        call_id="call-1",
                                        name="read_page",
                                        result={"text": "Page"},
                                    ),
                                ),
                            ),
                        ),
                        tools=(),
                        generation=GenerationOptions(),
                    )
                )
            ]
        assert events[-1] == ModelResponse(parts=(TextPart(text="Done"),))

    asyncio.run(exercise())


def test_gemini_normalizes_rate_limit_errors() -> None:
    async def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            429, json={"error": {"message": "quota"}}, headers={"retry-after": "2"}
        )

    async def exercise() -> None:
        async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as client:
            provider = GeminiProvider(api_key="test-key", client=client)
            request = ModelRequest(
                model=ModelRef(provider="gemini", model="gemini-3-flash-preview"),
                messages=(
                    Message(
                        role=MessageRole.USER,
                        parts=(TextPart(text="Hello"),),
                    ),
                ),
                tools=(),
                generation=GenerationOptions(),
            )

            with pytest.raises(RateLimitedError) as raised:
                _ = [event async for event in provider.stream(request)]

        assert raised.value.status_code == 429
        assert raised.value.retry_after == 2.0

    asyncio.run(exercise())


def test_gemini_preserves_tool_call_thought_signature() -> None:
    async def handler(request: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            text=(
                'data: {"candidates":[{"content":{"parts":[{'
                '"functionCall":{"name":"read_page","args":{}},'
                '"thoughtSignature":"signature-1"}]}}]}\n\n'
            ),
            headers={"content-type": "text/event-stream"},
        )

    async def exercise() -> None:
        async with httpx.AsyncClient(transport=httpx.MockTransport(handler)) as client:
            provider = GeminiProvider(api_key="test-key", client=client)
            events = [
                event
                async for event in provider.stream(
                    ModelRequest(
                        model=ModelRef(
                            provider="gemini", model="gemini-3-flash-preview"
                        ),
                        messages=(
                            Message(
                                role=MessageRole.USER,
                                parts=(TextPart(text="Read the page"),),
                            ),
                        ),
                        tools=(),
                        generation=GenerationOptions(),
                    )
                )
            ]

        assert events == [
            ModelResponse(
                parts=(
                    ToolCallPart(
                        call_id="gemini-1",
                        name="read_page",
                        arguments={},
                        provider_data={"thoughtSignature": "signature-1"},
                    ),
                )
            )
        ]

    asyncio.run(exercise())
