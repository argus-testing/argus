from __future__ import annotations

import os
from collections import Counter
from typing import Any
from urllib.parse import quote, urlsplit, urlunsplit

import httpx
from mcp.server.fastmcp import FastMCP
from mcp.server.fastmcp.exceptions import ToolError

DEFAULT_BASE_URL = "http://127.0.0.1:8000"
MAX_REPORT_TEXT = 4_000
MAX_DETAIL_TEXT = 2_000
MAX_REPORT_ITEMS = 20
MAX_EVENT_TYPES = 20
MAX_SCREENSHOTS = 50


class ArgusClient:
    def __init__(
        self,
        *,
        base_url: str = DEFAULT_BASE_URL,
        transport: httpx.AsyncBaseTransport | None = None,
    ) -> None:
        self.base_url = _validate_base_url(base_url)
        self.transport = transport

    @classmethod
    def from_env(cls) -> ArgusClient:
        return cls(base_url=os.environ.get("ARGUS_BASE_URL") or DEFAULT_BASE_URL)

    async def start_test(self, url: str, instructions: str) -> dict[str, Any]:
        run = _run_response(
            await self._request(
                "POST", "/api/runs", json={"url": url, "instructions": instructions}
            )
        )
        run_id = run["id"]
        return {
            "run_id": run_id,
            "status": run["status"],
            "polling_hint": f"Poll get_test_run with run_id '{run_id}' for updates.",
        }

    async def get_test_run(self, run_id: str) -> dict[str, Any]:
        return _run_response(
            await self._request("GET", _run_path(run_id), run_id=run_id)
        )

    async def list_test_runs(self, limit: int = 20) -> list[dict[str, Any]]:
        data = await self._request("GET", "/api/runs", params={"limit": limit})
        if not isinstance(data, list):
            raise ToolError("Argus returned an invalid run list response")
        return [_run_response(run) for run in data]

    async def cancel_test(self, run_id: str) -> dict[str, Any]:
        return _run_response(
            await self._request("POST", f"{_run_path(run_id)}/cancel", run_id=run_id)
        )

    async def get_test_evidence(self, run_id: str) -> dict[str, Any]:
        run = _run_response(
            await self._request("GET", _run_path(run_id), run_id=run_id)
        )
        events = run.get("events")
        if not isinstance(events, list):
            raise ToolError(
                "Argus returned an invalid evidence response: events missing"
            )

        event_types: Counter[str] = Counter()
        screenshots: list[dict[str, str]] = []
        for event in events:
            if not isinstance(event, dict) or not isinstance(event.get("type"), str):
                raise ToolError("Argus returned an invalid evidence event")
            event_type = event["type"]
            if len(event_type) > 100:
                raise ToolError("Argus returned an invalid evidence event type")
            event_types[event_type] += 1
            if (
                event_type != "browser.screenshot"
                or len(screenshots) >= MAX_SCREENSHOTS
            ):
                continue
            screenshot = self._screenshot(run_id, event)
            if screenshot is not None:
                screenshots.append(screenshot)

        by_type = dict(sorted(event_types.items())[:MAX_EVENT_TYPES])
        event_summary: dict[str, Any] = {"total": len(events), "by_type": by_type}
        if len(event_types) > MAX_EVENT_TYPES:
            event_summary["omitted_types"] = len(event_types) - MAX_EVENT_TYPES

        return {
            "run_id": run["id"],
            "status": run["status"],
            "report": _bounded_report(run.get("report")),
            "event_summary": event_summary,
            "screenshots": screenshots,
        }

    async def _request(
        self, method: str, path: str, *, run_id: str | None = None, **kwargs: Any
    ) -> Any:
        try:
            async with httpx.AsyncClient(
                base_url=self.base_url, transport=self.transport, timeout=10
            ) as client:
                response = await client.request(method, path, **kwargs)
        except httpx.RequestError as exc:
            raise ToolError(
                f"Argus server unavailable at {self.base_url}: {exc}"
            ) from exc

        if response.status_code == 404:
            if run_id is not None:
                raise ToolError(f"Argus run '{run_id}' was not found")
            raise ToolError(
                f"Argus API endpoint '{path}' was not found at {self.base_url}; "
                "check ARGUS_BASE_URL and server compatibility"
            )
        if response.is_error:
            raise ToolError(
                f"Argus API error {response.status_code}: {_api_error_detail(response)}"
            )
        try:
            return response.json()
        except ValueError as exc:
            raise ToolError("Argus returned invalid JSON") from exc

    def _screenshot(self, run_id: str, event: dict[str, Any]) -> dict[str, str] | None:
        data = event.get("data")
        if not isinstance(data, dict):
            return None
        path = data.get("path")
        prefix = f"/screenshots/{run_id}/"
        parsed = urlsplit(path) if isinstance(path, str) else None
        if (
            parsed is None
            or parsed.scheme
            or parsed.netloc
            or parsed.query
            or parsed.fragment
            or len(parsed.path) > 2_000
            or not parsed.path.startswith(prefix)
            or ".." in parsed.path.split("/")
        ):
            return None

        base = urlsplit(self.base_url)
        origin = urlunsplit((base.scheme, base.netloc, "", "", ""))
        screenshot = {"url": origin + quote(parsed.path, safe="/%:@-._~")}
        label = data.get("label")
        created_at = event.get("created_at")
        if isinstance(label, str):
            screenshot["label"] = _truncate(label, 200)
        if isinstance(created_at, str):
            screenshot["created_at"] = _truncate(created_at, 100)
        return {
            key: screenshot[key]
            for key in ("label", "created_at", "url")
            if key in screenshot
        }


def _validate_base_url(value: str) -> str:
    parsed = urlsplit(value)
    if (
        parsed.scheme not in {"http", "https"}
        or not parsed.hostname
        or parsed.username is not None
        or parsed.password is not None
        or parsed.query
        or parsed.fragment
    ):
        raise ValueError("ARGUS_BASE_URL must be an HTTP(S) URL without credentials")
    return value.rstrip("/")


def _run_path(run_id: str) -> str:
    return f"/api/runs/{quote(run_id, safe='')}"


def _run_response(data: Any) -> dict[str, Any]:
    if (
        not isinstance(data, dict)
        or not isinstance(data.get("id"), str)
        or len(data["id"]) > 128
        or not isinstance(data.get("status"), str)
        or len(data["status"]) > 100
    ):
        raise ToolError("Argus returned an invalid run response")
    return data


def _api_error_detail(response: httpx.Response) -> str:
    try:
        body = response.json()
    except ValueError:
        return _truncate(response.text.strip() or response.reason_phrase, 500)
    if isinstance(body, dict) and isinstance(body.get("detail"), str):
        return _truncate(body["detail"], 500)
    return "request failed"


def _bounded_report(report: Any) -> dict[str, Any] | None:
    if report is None:
        return None
    if not isinstance(report, dict):
        raise ToolError("Argus returned an invalid evidence response: report malformed")

    bounded: dict[str, Any] = {}
    for field, limit in (
        ("verdict", 100),
        ("summary", MAX_REPORT_TEXT),
        ("plan", MAX_REPORT_TEXT),
    ):
        value = report.get(field)
        if isinstance(value, str):
            bounded[field] = _truncate(value, limit)

    findings = report.get("findings")
    if isinstance(findings, list):
        bounded["findings"] = [
            {
                field: _truncate(finding[field], MAX_DETAIL_TEXT)
                for field in ("severity", "title", "detail")
                if isinstance(finding.get(field), str)
            }
            for finding in findings[:MAX_REPORT_ITEMS]
            if isinstance(finding, dict)
        ]

    recommendations = report.get("recommendations")
    if isinstance(recommendations, list):
        bounded["recommendations"] = [
            _truncate(item, MAX_DETAIL_TEXT)
            for item in recommendations[:MAX_REPORT_ITEMS]
            if isinstance(item, str)
        ]
    return bounded


def _truncate(value: str, limit: int) -> str:
    return value if len(value) <= limit else value[:limit] + "…"


def create_mcp_server(client: ArgusClient | None = None) -> FastMCP:
    api = client or ArgusClient.from_env()
    server = FastMCP(
        "Argus",
        instructions="Start and inspect test runs on a local Argus server.",
        log_level="ERROR",
    )

    @server.tool()
    async def start_test(url: str, instructions: str) -> dict[str, Any]:
        """Start an Argus UI test and return its durable run ID immediately."""
        return await api.start_test(url, instructions)

    @server.tool()
    async def get_test_run(run_id: str) -> dict[str, Any]:
        """Get the current canonical state of one Argus test run."""
        return await api.get_test_run(run_id)

    @server.tool()
    async def list_test_runs(limit: int = 20) -> list[dict[str, Any]]:
        """List recent Argus test runs, newest first."""
        return await api.list_test_runs(limit)

    @server.tool()
    async def cancel_test(run_id: str) -> dict[str, Any]:
        """Cancel a queued or running Argus test."""
        return await api.cancel_test(run_id)

    @server.tool()
    async def get_test_evidence(run_id: str) -> dict[str, Any]:
        """Get a bounded report, event summary, and screenshot URLs for a run."""
        return await api.get_test_evidence(run_id)

    return server


mcp = create_mcp_server()


def main() -> None:
    mcp.run(transport="stdio")


if __name__ == "__main__":
    main()
