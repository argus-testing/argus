from collections.abc import AsyncIterator, Mapping

from argus.runtime.errors import (
    InvalidProviderResponseError,
    ModelCallLimitExceeded,
    UnknownProviderError,
    UnknownToolError,
)
from argus.runtime.events import CompletedEvent, ToolCallEvent, ToolResultEvent
from argus.runtime.messages import (
    Message,
    MessageRole,
    TextPart,
    ToolCallPart,
    ToolResultPart,
)
from argus.runtime.models import AgentSpec
from argus.runtime.providers import (
    ModelProvider,
    ModelRequest,
    ModelResponse,
    TextDelta,
)
from argus.runtime.sessions import SessionStore
from argus.runtime.tools import Tool, ToolContext

RuntimeEvent = TextDelta | ToolCallEvent | ToolResultEvent | CompletedEvent


class AgentRuntime:
    def __init__(
        self,
        *,
        providers: Mapping[str, ModelProvider],
        sessions: SessionStore,
        max_model_calls: int = 32,
    ) -> None:
        self._providers = providers
        self._sessions = sessions
        self._max_model_calls = max_model_calls

    async def run(
        self,
        agent: AgentSpec,
        *,
        session_id: str,
        message: Message | None,
        state: Mapping[str, object] | None = None,
    ) -> AsyncIterator[RuntimeEvent]:
        provider = self._providers.get(agent.model.provider)
        if provider is None:
            raise UnknownProviderError(agent.model.provider)

        session = await self._sessions.get_or_create(session_id, state)
        if message is not None:
            await self._sessions.append(session_id, message)
        tools = self._tools_by_name(agent, session_id)

        for _ in range(self._max_model_calls):
            system_message = Message(
                role=MessageRole.SYSTEM,
                parts=(TextPart(text=agent.instruction),),
            )
            request = ModelRequest(
                model=agent.model,
                messages=(system_message, *session.messages),
                tools=tuple(tools.values()),
                generation=agent.generation,
            )
            response: ModelResponse | None = None
            async for event in provider.stream(request):
                if isinstance(event, TextDelta):
                    yield event
                elif isinstance(event, ModelResponse):
                    response = event

            if response is None:
                raise InvalidProviderResponseError(
                    "Provider did not return a final response"
                )

            assistant_message = Message(
                role=MessageRole.ASSISTANT,
                parts=response.parts,
            )
            await self._sessions.append(session_id, assistant_message)
            tool_calls = [
                part for part in response.parts if isinstance(part, ToolCallPart)
            ]
            if not tool_calls:
                yield CompletedEvent(message=assistant_message)
                return

            for call in tool_calls:
                yield ToolCallEvent(call=call)
                tool = tools.get(call.name)
                if tool is None:
                    raise UnknownToolError(call.name)
                result_part = ToolResultPart(
                    call_id=call.call_id,
                    name=call.name,
                    result=await tool.invoke(
                        call.arguments, ToolContext(state=session.state)
                    ),
                )
                await self._sessions.append(
                    session_id,
                    Message(role=MessageRole.TOOL, parts=(result_part,)),
                )
                yield ToolResultEvent(result=result_part)

        raise ModelCallLimitExceeded(self._max_model_calls)

    def _tools_by_name(self, agent: AgentSpec, session_id: str) -> dict[str, Tool]:
        tools: dict[str, Tool] = {}
        for tool in agent.tools:
            if not isinstance(tool, Tool):
                raise TypeError(f"Agent tool must be Tool, got {type(tool).__name__}")
            tools[tool.name] = tool
        for sub_agent in agent.sub_agents:
            delegation = self._delegation_tool(sub_agent, session_id)
            tools[delegation.name] = delegation
        return tools

    def _delegation_tool(self, sub_agent: AgentSpec, session_id: str) -> Tool:
        async def delegate(task: str, tool_context: ToolContext) -> dict[str, str]:
            message = Message(
                role=MessageRole.USER,
                parts=(TextPart(text=task),),
            )
            output = ""
            async for event in self.run(
                sub_agent,
                session_id=f"{session_id}:subagent:{sub_agent.name}",
                message=message,
                state=tool_context.state,
            ):
                if isinstance(event, CompletedEvent):
                    output = "".join(
                        part.text
                        for part in event.message.parts
                        if isinstance(part, TextPart)
                    )
            return {"output": output}

        delegate.__name__ = f"delegate_to_{sub_agent.name}"
        delegate.__doc__ = f"Delegate a task to the {sub_agent.name} agent."
        return Tool.from_callable(delegate)
