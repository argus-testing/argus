import asyncio
from collections.abc import AsyncIterator

from argus.runtime.messages import Message, MessageRole, TextPart
from argus.runtime.models import GenerationOptions, ModelRef
from argus.runtime.providers import ModelRequest, ModelResponse, TextDelta


class FakeProvider:
    async def stream(
        self, request: ModelRequest
    ) -> AsyncIterator[TextDelta | ModelResponse]:
        assert request.model.provider == "test"
        yield TextDelta(text="Hel")
        yield ModelResponse(parts=(TextPart(text="Hello"),))


def test_provider_stream_uses_normalized_requests_and_events() -> None:
    request = ModelRequest(
        model=ModelRef(provider="test", model="small"),
        messages=(Message(role=MessageRole.USER, parts=(TextPart(text="Hi"),)),),
        tools=(),
        generation=GenerationOptions(),
    )

    async def collect() -> list[TextDelta | ModelResponse]:
        return [event async for event in FakeProvider().stream(request)]

    events = asyncio.run(collect())

    assert events == [
        TextDelta(text="Hel"),
        ModelResponse(parts=(TextPart(text="Hello"),)),
    ]
