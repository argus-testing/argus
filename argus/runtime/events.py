from dataclasses import dataclass

from argus.runtime.messages import Message, ToolCallPart, ToolResultPart


@dataclass(frozen=True, slots=True)
class ToolCallEvent:
    call: ToolCallPart


@dataclass(frozen=True, slots=True)
class ToolResultEvent:
    result: ToolResultPart


@dataclass(frozen=True, slots=True)
class CompletedEvent:
    message: Message
