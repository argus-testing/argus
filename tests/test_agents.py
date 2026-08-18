from argus.agents import (
    build_comprehender_agent,
    build_critic_agent,
    build_executor_agent,
    build_explorer_agent,
    build_strategist_agent,
    build_validator_agent,
)
from argus.runtime.models import ModelRef
from argus.runtime.tools import Tool


def test_validator_agent_spec():
    model = ModelRef(provider="gemini", model="gemini-2.5-flash")
    agent = build_validator_agent(model)
    assert agent.name == "validator"
    assert agent.model == model
    assert len(agent.tools) == 0
    assert "Request Validator" in agent.instruction


def test_comprehender_agent_spec():
    model = ModelRef(provider="gemini", model="gemini-2.5-flash")
    agent = build_comprehender_agent(model)
    assert agent.name == "comprehender"
    assert agent.model == model
    assert len(agent.tools) == 0
    assert "Intent Comprehender" in agent.instruction


def test_explorer_agent_spec():
    model = ModelRef(provider="gemini", model="gemini-2.5-flash")
    tools = (Tool.from_callable(lambda: "ok"),)
    agent = build_explorer_agent(model, tools)
    assert agent.name == "explorer"
    assert agent.model == model
    assert len(agent.tools) == 1
    assert "App Explorer" in agent.instruction


def test_strategist_agent_spec():
    model = ModelRef(provider="gemini", model="gemini-2.5-flash")
    agent = build_strategist_agent(model)
    assert agent.name == "strategist"
    assert agent.model == model
    assert len(agent.tools) == 0
    assert "Test Strategist" in agent.instruction


def test_executor_agent_spec():
    model = ModelRef(provider="gemini", model="gemini-2.5-flash")
    tools = (Tool.from_callable(lambda: "ok"),)
    agent = build_executor_agent(model, tools)
    assert agent.name == "executor"
    assert agent.model == model
    assert len(agent.tools) == 1
    assert "Test Executor" in agent.instruction


def test_critic_agent_spec():
    model = ModelRef(provider="gemini", model="gemini-2.5-flash")
    agent = build_critic_agent(model)
    assert agent.name == "critic"
    assert agent.model == model
    assert len(agent.tools) == 0
    assert "QA Critic" in agent.instruction
