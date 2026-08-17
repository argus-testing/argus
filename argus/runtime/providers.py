from collections.abc import AsyncIterator
from dataclasses import dataclass
from typing import Protocol

from argus.runtime.messages import Message, TextPart, ToolCallPart
from argus.runtime.models import GenerationOptions, ModelRef


@dataclass(frozen=True, slots=True)
class ModelRequest:
    model: ModelRef
    messages: tuple[Message, ...]
    tools: tuple[object, ...]
    generation: GenerationOptions


@dataclass(frozen=True, slots=True)
class TextDelta:
    text: str


@dataclass(frozen=True, slots=True)
class ModelResponse:
    parts: tuple[TextPart | ToolCallPart, ...]


ProviderEvent = TextDelta | ModelResponse


class ModelProvider(Protocol):
    def stream(self, request: ModelRequest) -> AsyncIterator[ProviderEvent]: ...
