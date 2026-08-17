import asyncio
from collections.abc import AsyncIterator

import pytest

from argus.runtime.errors import InvalidProviderResponseError
from argus.runtime.events import CompletedEvent, ToolCallEvent, ToolResultEvent
from argus.runtime.messages import (
    Message,
    MessageRole,
    TextPart,
    ToolCallPart,
    ToolResultPart,
)
from argus.runtime.models import AgentSpec, ModelRef
from argus.runtime.providers import ModelRequest, ModelResponse, TextDelta
from argus.runtime.runtime import AgentRuntime
from argus.runtime.sessions import InMemorySessionStore
from argus.runtime.tools import Tool, ToolContext


class ToolCallingProvider:
    async def stream(
        self, request: ModelRequest
    ) -> AsyncIterator[TextDelta | ModelResponse]:
        has_result = any(
            isinstance(part, ToolResultPart)
            for message in request.messages
            for part in message.parts
        )
        if not has_result:
            yield ModelResponse(
                parts=(
                    ToolCallPart(
                        call_id="call-1",
                        name="read_page",
                        arguments={},
                    ),
                )
            )
            return
        yield TextDelta(text="Verified")
        yield ModelResponse(parts=(TextPart(text="Verified"),))


class StallingProvider(ToolCallingProvider):
    def __init__(self) -> None:
        self.stalled = False

    async def stream(
        self, request: ModelRequest
    ) -> AsyncIterator[TextDelta | ModelResponse]:
        has_result = any(
            isinstance(part, ToolResultPart)
            for message in request.messages
            for part in message.parts
        )
        if has_result and not self.stalled:
            self.stalled = True
            return
        async for event in super().stream(request):
            yield event


class DelegatingProvider:
    async def stream(
        self, request: ModelRequest
    ) -> AsyncIterator[TextDelta | ModelResponse]:
        if request.model.model == "navigator":
            yield ModelResponse(parts=(TextPart(text="Reached the home page"),))
            return
        has_result = any(
            isinstance(part, ToolResultPart)
            for message in request.messages
            for part in message.parts
        )
        if not has_result:
            yield ModelResponse(
                parts=(
                    ToolCallPart(
                        call_id="delegate-1",
                        name="delegate_to_navigator",
                        arguments={"task": "Navigate home"},
                    ),
                )
            )
            return
        yield ModelResponse(parts=(TextPart(text="Navigation verified"),))


def test_runtime_executes_tools_and_persists_results_before_continuing() -> None:
    async def exercise() -> None:
        calls = 0

        async def read_page(tool_context: ToolContext) -> dict:
            nonlocal calls
            calls += 1
            return {"run_id": tool_context.state["run_id"], "text": "Welcome"}

        sessions = InMemorySessionStore()
        runtime = AgentRuntime(
            providers={"test": ToolCallingProvider()},
            sessions=sessions,
        )
        agent = AgentSpec(
            name="executor",
            model=ModelRef(provider="test", model="tool-model"),
            instruction="Verify the page.",
            tools=(Tool.from_callable(read_page),),
        )
        user_message = Message(
            role=MessageRole.USER,
            parts=(TextPart(text="Test the home page"),),
        )

        events = [
            event
            async for event in runtime.run(
                agent,
                session_id="run-1",
                message=user_message,
                state={"run_id": "run-1"},
            )
        ]
        session = await sessions.get_or_create("run-1")

        assert calls == 1
        assert [type(event) for event in events] == [
            ToolCallEvent,
            ToolResultEvent,
            TextDelta,
            CompletedEvent,
        ]
        assert [message.role for message in session.messages] == [
            MessageRole.USER,
            MessageRole.ASSISTANT,
            MessageRole.TOOL,
            MessageRole.ASSISTANT,
        ]
        assert session.messages[2].parts == (
            ToolResultPart(
                call_id="call-1",
                name="read_page",
                result={"run_id": "run-1", "text": "Welcome"},
            ),
        )

    asyncio.run(exercise())


def test_runtime_continues_stalled_session_without_replaying_tool() -> None:
    async def exercise() -> None:
        calls = 0

        async def read_page(tool_context: ToolContext) -> dict:
            nonlocal calls
            calls += 1
            return {"saved": True}

        sessions = InMemorySessionStore()
        runtime = AgentRuntime(
            providers={"test": StallingProvider()},
            sessions=sessions,
        )
        agent = AgentSpec(
            name="executor",
            model=ModelRef(provider="test", model="tool-model"),
            instruction="Save once.",
            tools=(Tool.from_callable(read_page),),
        )
        message = Message(
            role=MessageRole.USER,
            parts=(TextPart(text="Save the form"),),
        )

        with pytest.raises(InvalidProviderResponseError):
            _ = [
                event
                async for event in runtime.run(
                    agent,
                    session_id="run-2",
                    message=message,
                )
            ]

        events = [
            event
            async for event in runtime.run(
                agent,
                session_id="run-2",
                message=None,
            )
        ]
        session = await sessions.get_or_create("run-2")

        assert calls == 1
        assert isinstance(events[-1], CompletedEvent)
        assert all(isinstance(item, Message) for item in session.messages)
        assert sum(item.role is MessageRole.USER for item in session.messages) == 1

    asyncio.run(exercise())


def test_runtime_exposes_sub_agents_as_delegation_tools() -> None:
    async def exercise() -> None:
        sessions = InMemorySessionStore()
        runtime = AgentRuntime(
            providers={"test": DelegatingProvider()},
            sessions=sessions,
        )
        navigator = AgentSpec(
            name="navigator",
            model=ModelRef(provider="test", model="navigator"),
            instruction="Navigate to the requested page.",
        )
        executor = AgentSpec(
            name="executor",
            model=ModelRef(provider="test", model="executor"),
            instruction="Test the page.",
            sub_agents=(navigator,),
        )

        events = [
            event
            async for event in runtime.run(
                executor,
                session_id="run-3",
                message=Message(
                    role=MessageRole.USER,
                    parts=(TextPart(text="Test navigation"),),
                ),
            )
        ]
        parent = await sessions.get_or_create("run-3")
        child = await sessions.get_or_create("run-3:subagent:navigator")

        assert isinstance(events[-1], CompletedEvent)
        assert child.messages[-1].parts == (TextPart(text="Reached the home page"),)
        assert parent.messages[2].parts == (
            ToolResultPart(
                call_id="delegate-1",
                name="delegate_to_navigator",
                result={"output": "Reached the home page"},
            ),
        )

    asyncio.run(exercise())
