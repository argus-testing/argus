from collections.abc import Mapping
from dataclasses import dataclass, field
from enum import StrEnum


class MessageRole(StrEnum):
    SYSTEM = "system"
    USER = "user"
    ASSISTANT = "assistant"
    TOOL = "tool"


@dataclass(frozen=True, slots=True)
class TextPart:
    text: str


@dataclass(frozen=True, slots=True)
class ImagePart:
    data: bytes
    media_type: str


@dataclass(frozen=True, slots=True)
class AudioPart:
    data: bytes
    media_type: str


@dataclass(frozen=True, slots=True)
class ToolCallPart:
    call_id: str
    name: str
    arguments: Mapping[str, object]
    provider_data: Mapping[str, object] = field(default_factory=dict)


@dataclass(frozen=True, slots=True)
class ToolResultPart:
    call_id: str
    name: str
    result: object


MessagePart = TextPart | ImagePart | AudioPart | ToolCallPart | ToolResultPart


@dataclass(frozen=True, slots=True)
class Message:
    role: MessageRole
    parts: tuple[MessagePart, ...]
