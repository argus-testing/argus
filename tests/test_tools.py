import asyncio

from argus.runtime.tools import Tool, ToolContext


async def read_page(
    include_forms: bool = False, tool_context: ToolContext | None = None
) -> dict:
    """Read visible content from the current page."""
    return {
        "include_forms": include_forms,
        "run_id": tool_context.state["run_id"] if tool_context else None,
    }


def test_tool_builds_schema_without_exposing_runtime_context() -> None:
    tool = Tool.from_callable(read_page)

    assert tool.name == "read_page"
    assert tool.description == "Read visible content from the current page."
    assert tool.input_schema == {
        "type": "object",
        "properties": {"include_forms": {"type": "boolean", "default": False}},
        "required": [],
        "additionalProperties": False,
    }

    result = asyncio.run(
        tool.invoke(
            {"include_forms": True},
            ToolContext(state={"run_id": "run-1"}),
        )
    )

    assert result == {"include_forms": True, "run_id": "run-1"}


def test_tool_invokes_functions_that_do_not_accept_context() -> None:
    def add(left: int, right: int) -> int:
        """Add two numbers."""
        return left + right

    tool = Tool.from_callable(add)
    result = asyncio.run(tool.invoke({"left": 2, "right": 3}, ToolContext(state={})))

    assert result == 5
