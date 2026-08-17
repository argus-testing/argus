import inspect
from collections.abc import Callable, Mapping
from dataclasses import dataclass
from typing import Any, get_type_hints


@dataclass(slots=True)
class ToolContext:
    state: dict[str, object]


@dataclass(frozen=True, slots=True)
class Tool:
    name: str
    description: str
    input_schema: Mapping[str, object]
    func: Callable[..., object]
    _accepts_context: bool

    @classmethod
    def from_callable(cls, func: Callable[..., object]) -> "Tool":
        signature = inspect.signature(func)
        hints = get_type_hints(func)
        properties: dict[str, object] = {}
        required: list[str] = []

        for name, parameter in signature.parameters.items():
            if name == "tool_context":
                continue
            schema = _annotation_schema(hints.get(name, Any))
            if parameter.default is inspect.Parameter.empty:
                required.append(name)
            else:
                schema["default"] = parameter.default
            properties[name] = schema

        doc = inspect.getdoc(func) or ""
        return cls(
            name=func.__name__,
            description=doc.splitlines()[0] if doc else "",
            input_schema={
                "type": "object",
                "properties": properties,
                "required": required,
                "additionalProperties": False,
            },
            func=func,
            _accepts_context="tool_context" in signature.parameters,
        )

    async def invoke(
        self,
        arguments: Mapping[str, object],
        context: ToolContext,
    ) -> object:
        kwargs = dict(arguments)
        if self._accepts_context:
            kwargs["tool_context"] = context
        result = self.func(**kwargs)
        if inspect.isawaitable(result):
            return await result
        return result


def _annotation_schema(annotation: object) -> dict[str, object]:
    json_type = {
        str: "string",
        bool: "boolean",
        int: "integer",
        float: "number",
        dict: "object",
        list: "array",
    }.get(annotation, "string")
    return {"type": json_type}
