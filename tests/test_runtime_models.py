from argus.runtime.models import AgentSpec, GenerationOptions, ModelRef, ReasoningEffort


def test_agent_spec_uses_explicit_provider_and_model() -> None:
    agent = AgentSpec(
        name="executor",
        model=ModelRef(provider="gemini", model="gemini-3-flash-preview"),
        instruction="Test the requested feature.",
    )

    assert agent.model.provider == "gemini"
    assert agent.model.model == "gemini-3-flash-preview"


def test_agent_spec_has_provider_neutral_runtime_defaults() -> None:
    agent = AgentSpec(
        name="critic",
        model=ModelRef(provider="openai", model="gpt-5"),
        instruction="Review the result.",
    )

    assert agent.tools == ()
    assert agent.sub_agents == ()
    assert agent.output_schema is None
    assert agent.generation == GenerationOptions()


def test_generation_options_express_portable_capability_hints() -> None:
    options = GenerationOptions(
        temperature=0.2,
        max_output_tokens=4096,
        reasoning_effort=ReasoningEffort.MEDIUM,
        json_mode=True,
    )

    assert options.temperature == 0.2
    assert options.max_output_tokens == 4096
    assert options.reasoning_effort is ReasoningEffort.MEDIUM
    assert options.json_mode is True
