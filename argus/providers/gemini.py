import base64
import json
from collections.abc import AsyncIterator, Mapping

import httpx

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
from argus.runtime.providers import (
    ModelRequest,
    ModelResponse,
    ProviderEvent,
    TextDelta,
)
from argus.runtime.tools import Tool


class GeminiProvider:
    def __init__(
        self,
        *,
        api_key: str,
        client: httpx.AsyncClient | None = None,
        base_url: str = "https://generativelanguage.googleapis.com",
    ) -> None:
        self._api_key = api_key
        self._client = client
        self._base_url = base_url.rstrip("/")

    async def stream(self, request: ModelRequest) -> AsyncIterator[ProviderEvent]:
        client = self._client or httpx.AsyncClient(timeout=45)
        owns_client = self._client is None
        url = (
            f"{self._base_url}/v1beta/models/{request.model.model}"
            ":streamGenerateContent"
        )
        text = ""
        tool_calls: list[ToolCallPart] = []
        try:
            async with client.stream(
                "POST",
                url,
                params={"alt": "sse"},
                headers={"x-goog-api-key": self._api_key},
                json=self._payload(request),
            ) as response:
                if response.status_code == 429:
                    raise RateLimitedError(
                        "Gemini rate limit exceeded",
                        status_code=response.status_code,
                        retry_after=_retry_after(response),
                    )
                response.raise_for_status()
                async for line in response.aiter_lines():
                    if not line.startswith("data: "):
                        continue
                    data = line[6:]
                    if data == "[DONE]":
                        continue
                    chunk = json.loads(data)
                    for part in _response_parts(chunk):
                        part_text = part.get("text")
                        if isinstance(part_text, str):
                            text += part_text
                            yield TextDelta(text=part_text)
                        function_call = part.get("functionCall")
                        if isinstance(function_call, Mapping):
                            provider_data = {}
                            thought_signature = part.get("thoughtSignature")
                            if isinstance(thought_signature, str):
                                provider_data["thoughtSignature"] = thought_signature
                            tool_calls.append(
                                ToolCallPart(
                                    call_id=str(
                                        function_call.get("id")
                                        or f"gemini-{len(tool_calls) + 1}"
                                    ),
                                    name=str(function_call["name"]),
                                    arguments=dict(function_call.get("args") or {}),
                                    provider_data=provider_data,
                                )
                            )
        finally:
            if owns_client:
                await client.aclose()

        parts: list[TextPart | ToolCallPart] = []
        if text:
            parts.append(TextPart(text=text))
        parts.extend(tool_calls)
        yield ModelResponse(parts=tuple(parts))

    def _payload(self, request: ModelRequest) -> dict[str, object]:
        system_parts: list[dict[str, object]] = []
        contents: list[dict[str, object]] = []
        for message in request.messages:
            if message.role is MessageRole.SYSTEM:
                system_parts.extend(_message_parts(message))
                continue
            contents.append(
                {
                    "role": (
                        "model" if message.role is MessageRole.ASSISTANT else "user"
                    ),
                    "parts": _message_parts(message),
                }
            )

        payload: dict[str, object] = {"contents": contents}
        if system_parts:
            payload["systemInstruction"] = {"parts": system_parts}
        tools = [
            _tool_declaration(tool) for tool in request.tools if isinstance(tool, Tool)
        ]
        if tools:
            payload["tools"] = [{"functionDeclarations": tools}]

        config: dict[str, object] = {}
        generation = request.generation
        if generation.temperature is not None:
            config["temperature"] = generation.temperature
        if generation.max_output_tokens is not None:
            config["maxOutputTokens"] = generation.max_output_tokens
        if generation.json_mode:
            config["responseMimeType"] = "application/json"
        if config:
            payload["generationConfig"] = config
        return payload


def _message_parts(message: Message) -> list[dict[str, object]]:
    parts: list[dict[str, object]] = []
    for part in message.parts:
        if isinstance(part, TextPart):
            parts.append({"text": part.text})
        elif isinstance(part, (ImagePart, AudioPart)):
            parts.append(
                {
                    "inlineData": {
                        "mimeType": part.media_type,
                        "data": base64.b64encode(part.data).decode("ascii"),
                    }
                }
            )
        elif isinstance(part, ToolCallPart):
            function_part: dict[str, object] = {
                "functionCall": {
                    "id": part.call_id,
                    "name": part.name,
                    "args": dict(part.arguments),
                }
            }
            thought_signature = part.provider_data.get("thoughtSignature")
            if isinstance(thought_signature, str):
                function_part["thoughtSignature"] = thought_signature
            parts.append(function_part)
        elif isinstance(part, ToolResultPart):
            response = (
                dict(part.result)
                if isinstance(part.result, Mapping)
                else {"result": part.result}
            )
            parts.append(
                {
                    "functionResponse": {
                        "id": part.call_id,
                        "name": part.name,
                        "response": response,
                    }
                }
            )
    return parts


def _tool_declaration(tool: Tool) -> dict[str, object]:
    return {
        "name": tool.name,
        "description": tool.description,
        "parameters": dict(tool.input_schema),
    }


def _response_parts(chunk: object) -> list[Mapping[str, object]]:
    if not isinstance(chunk, Mapping):
        return []
    candidates = chunk.get("candidates")
    if not isinstance(candidates, list) or not candidates:
        return []
    candidate = candidates[0]
    if not isinstance(candidate, Mapping):
        return []
    content = candidate.get("content")
    if not isinstance(content, Mapping):
        return []
    parts = content.get("parts")
    if not isinstance(parts, list):
        return []
    return [part for part in parts if isinstance(part, Mapping)]


def _retry_after(response: httpx.Response) -> float | None:
    value = response.headers.get("retry-after")
    if value is None:
        return None
    try:
        return float(value)
    except ValueError:
        return None
