from dataclasses import dataclass
from enum import StrEnum


@dataclass(frozen=True, slots=True)
class ModelRef:
    provider: str
    model: str


class ReasoningEffort(StrEnum):
    LOW = "low"
    MEDIUM = "medium"
    HIGH = "high"


@dataclass(frozen=True, slots=True)
class GenerationOptions:
    temperature: float | None = None
    max_output_tokens: int | None = None
    reasoning_effort: ReasoningEffort | None = None
    json_mode: bool = False


@dataclass(frozen=True, slots=True)
class AgentSpec:
    name: str
    model: ModelRef
    instruction: str
    tools: tuple[object, ...] = ()
    sub_agents: tuple["AgentSpec", ...] = ()
    output_schema: type[object] | None = None
    generation: GenerationOptions = GenerationOptions()
