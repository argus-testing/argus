from __future__ import annotations

import json
from importlib.metadata import distribution
from typing import Any

import httpx
import pytest
from mcp.server.fastmcp.exceptions import ToolError

from argus import mcp_server
from argus.mcp_server import ArgusClient, create_mcp_server


def mock_client(
    handler,
    *,
    base_url: str = "http://127.0.0.1:8000",
) -> ArgusClient:
    return ArgusClient(base_url=base_url, transport=httpx.MockTransport(handler))


async def tool_result(server, name: str, arguments: dict[str, Any]) -> Any:
    _, structured = await server.call_tool(name, arguments)
    return structured.get("result", structured)


@pytest.mark.asyncio
async def test_exposes_exactly_five_high_level_tools():
    server = create_mcp_server(mock_client(lambda _: httpx.Response(500)))

    assert {tool.name for tool in await server.list_tools()} == {
        "start_test",
        "get_test_run",
        "list_test_runs",
        "cancel_test",
        "get_test_evidence",
    }


@pytest.mark.asyncio
async def test_start_test_maps_request_and_returns_polling_hint():
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.method == "POST"
        assert request.url == "http://127.0.0.1:8000/api/runs"
        assert json.loads(request.content) == {
            "url": "https://example.com",
            "instructions": "Check navigation",
        }
        return httpx.Response(201, json={"id": "run-1", "status": "queued"})

    result = await tool_result(
        create_mcp_server(mock_client(handler)),
        "start_test",
        {"url": "https://example.com", "instructions": "Check navigation"},
    )

    assert result == {
        "run_id": "run-1",
        "status": "queued",
        "polling_hint": "Poll get_test_run with run_id 'run-1' for updates.",
    }


@pytest.mark.asyncio
async def test_get_list_and_cancel_map_canonical_api_responses():
    seen: list[tuple[str, str]] = []

    def handler(request: httpx.Request) -> httpx.Response:
        seen.append((request.method, str(request.url)))
        if request.url.path == "/api/runs/run-1/cancel":
            return httpx.Response(200, json={"id": "run-1", "status": "cancelled"})
        if request.url.path == "/api/runs/run-1":
            return httpx.Response(200, json={"id": "run-1", "status": "running"})
        return httpx.Response(
            200,
            json=[
                {"id": "run-2", "status": "passed"},
                {"id": "run-1", "status": "running"},
            ],
        )

    server = create_mcp_server(mock_client(handler))
    assert await tool_result(server, "get_test_run", {"run_id": "run-1"}) == {
        "id": "run-1",
        "status": "running",
    }
    assert await tool_result(server, "list_test_runs", {"limit": 7}) == [
        {"id": "run-2", "status": "passed"},
        {"id": "run-1", "status": "running"},
    ]
    assert await tool_result(server, "cancel_test", {"run_id": "run-1"}) == {
        "id": "run-1",
        "status": "cancelled",
    }
    assert seen == [
        ("GET", "http://127.0.0.1:8000/api/runs/run-1"),
        ("GET", "http://127.0.0.1:8000/api/runs?limit=7"),
        ("POST", "http://127.0.0.1:8000/api/runs/run-1/cancel"),
    ]


@pytest.mark.asyncio
async def test_list_test_runs_defaults_to_twenty():
    def handler(request: httpx.Request) -> httpx.Response:
        assert request.url.query == b"limit=20"
        return httpx.Response(200, json=[])

    result = await tool_result(
        create_mcp_server(mock_client(handler)), "list_test_runs", {}
    )
    assert result == []


def test_base_url_defaults_from_environment(monkeypatch):
    monkeypatch.delenv("ARGUS_BASE_URL", raising=False)
    assert ArgusClient.from_env().base_url == "http://127.0.0.1:8000"

    monkeypatch.setenv("ARGUS_BASE_URL", "http://localhost:9000/")
    assert ArgusClient.from_env().base_url == "http://localhost:9000"


@pytest.mark.asyncio
async def test_evidence_is_bounded_and_only_returns_safe_screenshot_urls():
    long_text = "x" * 10_000
    raw_page = "PRIVATE INSPECTED PAGE CONTENT"

    def handler(_: httpx.Request) -> httpx.Response:
        return httpx.Response(
            200,
            json={
                "id": "run-1",
                "status": "passed",
                "report": {
                    "verdict": "passed",
                    "summary": long_text,
                    "findings": [
                        {"severity": "high", "title": long_text, "detail": long_text}
                    ],
                    "recommendations": [long_text],
                    "plan": long_text,
                },
                "events": [
                    {
                        "type": "browser.observation",
                        "created_at": "2025-01-01T00:00:00Z",
                        "data": {"result": raw_page},
                    },
                    {
                        "type": "browser.screenshot",
                        "created_at": "2025-01-01T00:00:01Z",
                        "data": {
                            "path": "/screenshots/run-1/final.png",
                            "label": "Final page",
                        },
                    },
                    {
                        "type": "browser.screenshot",
                        "created_at": "2025-01-01T00:00:02Z",
                        "data": {
                            "path": "https://evil.example/stolen.png",
                            "label": "Unsafe",
                        },
                    },
                ],
            },
        )

    result = await tool_result(
        create_mcp_server(
            mock_client(handler, base_url="http://localhost:9000/prefix")
        ),
        "get_test_evidence",
        {"run_id": "run-1"},
    )

    assert result["run_id"] == "run-1"
    assert result["status"] == "passed"
    assert len(result["report"]["summary"]) <= 4_001
    assert len(result["report"]["findings"][0]["detail"]) <= 2_001
    assert len(result["report"]["recommendations"][0]) <= 2_001
    assert len(result["report"]["plan"]) <= 4_001
    assert result["event_summary"] == {
        "total": 3,
        "by_type": {"browser.observation": 1, "browser.screenshot": 2},
    }
    assert result["screenshots"] == [
        {
            "label": "Final page",
            "created_at": "2025-01-01T00:00:01Z",
            "url": "http://localhost:9000/screenshots/run-1/final.png",
        }
    ]
    assert raw_page not in json.dumps(result)
    assert "evil.example" not in json.dumps(result)


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("tool_name", "arguments"),
    [
        (
            "start_test",
            {"url": "https://example.com", "instructions": "Check navigation"},
        ),
        ("list_test_runs", {}),
    ],
)
async def test_collection_404_reports_api_incompatibility(tool_name, arguments):
    server = create_mcp_server(
        mock_client(
            lambda request: httpx.Response(
                404, json={"detail": "Not Found"}, request=request
            )
        )
    )

    with pytest.raises(ToolError, match="Argus API endpoint '/api/runs' was not found"):
        await server.call_tool(tool_name, arguments)


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("tool_name", "arguments"),
    [
        ("get_test_run", {"run_id": "missing"}),
        ("cancel_test", {"run_id": "missing"}),
        ("get_test_evidence", {"run_id": "missing"}),
    ],
)
async def test_item_404_reports_run_not_found(tool_name, arguments):
    server = create_mcp_server(
        mock_client(
            lambda request: httpx.Response(
                404, json={"detail": "Run not found"}, request=request
            )
        )
    )

    with pytest.raises(ToolError, match="run 'missing' was not found"):
        await server.call_tool(tool_name, arguments)


@pytest.mark.asyncio
@pytest.mark.parametrize(
    ("handler", "message"),
    [
        (
            lambda request: httpx.Response(
                404, json={"detail": "Run not found"}, request=request
            ),
            "run 'missing' was not found",
        ),
        (
            lambda request: httpx.Response(
                500, json={"detail": "database unhappy"}, request=request
            ),
            "Argus API error 500: database unhappy",
        ),
        (
            lambda request: httpx.Response(200, text="not-json", request=request),
            "invalid JSON",
        ),
        (
            lambda request: httpx.Response(
                200, json={"unexpected": True}, request=request
            ),
            "invalid run response",
        ),
    ],
)
async def test_api_errors_are_clear(handler, message):
    server = create_mcp_server(mock_client(handler))
    with pytest.raises(ToolError, match=message):
        await server.call_tool("get_test_run", {"run_id": "missing"})


@pytest.mark.asyncio
async def test_server_unavailable_error_is_clear():
    def handler(request: httpx.Request) -> httpx.Response:
        raise httpx.ConnectError("connection refused", request=request)

    with pytest.raises(ToolError, match="Argus server unavailable at"):
        await create_mcp_server(mock_client(handler)).call_tool(
            "get_test_run", {"run_id": "run-1"}
        )


def test_cli_entry_point_runs_stdio(monkeypatch):
    entry_points = {
        entry.name: entry.value for entry in distribution("argus").entry_points
    }
    assert entry_points["argus-mcp"] == "argus.mcp_server:main"

    called: list[str] = []
    monkeypatch.setattr(
        mcp_server.mcp, "run", lambda transport: called.append(transport)
    )
    mcp_server.main()
    assert called == ["stdio"]
