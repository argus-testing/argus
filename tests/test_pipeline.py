import json

import pytest

from argus.config import Settings
from argus.pipeline import (
    RunExecutor,
    _browser_page,
    _navigation_url,
    _persisted_tool_result,
    _tool_event_arguments,
)
from argus.store import RunStatus, RunStore


@pytest.mark.asyncio
async def test_missing_key_persists_failed_run_and_event(tmp_path):
    store = RunStore(tmp_path / "argus.db")
    await store.initialize()
    run = await store.create_run("https://example.com", "Check the page")

    async def emit(run_id, event_type, data):
        await store.add_event(run_id, event_type, data)

    executor = RunExecutor(store, Settings(data_dir=tmp_path), emit)
    await executor.run(run["id"])

    failed = await store.get_run(run["id"], events=True)
    assert failed is not None
    assert failed["status"] == RunStatus.FAILED
    assert failed["error"] == "GEMINI_API_KEY is not configured"
    assert failed["events"][-1]["data"]["kind"] == "configuration"


def test_report_falls_back_to_inconclusive_for_invalid_json():
    report = RunExecutor._report("not json", "inspect page", "page loaded")
    assert report == {
        "verdict": "inconclusive",
        "summary": "not json",
        "findings": [],
        "recommendations": [],
        "plan": "inspect page",
    }


@pytest.mark.asyncio
async def test_unsafe_navigation_is_rejected_before_event_persistence(tmp_path):
    store = RunStore(tmp_path / "argus.db")
    await store.initialize()
    run = await store.create_run("https://example.com", "Check the page")

    for unsafe_url in (
        "https://user:secret@example.com/account",
        "https://example.com/account?access_token=secret",
    ):
        with pytest.raises(ValueError):
            await store.add_event(
                run["id"],
                "browser.action",
                {
                    "tool": "navigate",
                    "arguments": _tool_event_arguments("navigate", {"url": unsafe_url}),
                },
            )
        with pytest.raises(ValueError):
            _navigation_url(unsafe_url)

    assert await store.events_after(run["id"]) == []


def test_tool_event_arguments_do_not_persist_typed_text():
    assert _tool_event_arguments(
        "type_text", {"selector": "#password", "text": "secret"}
    ) == {"selector": "#password", "text": "[redacted]"}


def test_inspect_observation_omits_raw_page_content():
    result = _persisted_tool_result(
        "inspect_page",
        {
            "url": "https://example.com/?token=secret",
            "text": "private page contents",
            "interactive": "password value",
        },
    )
    assert result == {
        "omitted": True,
        "summary": "Page inspection omitted from persisted events",
    }


@pytest.mark.asyncio
async def test_browser_cleanup_attempts_browser_close_when_context_close_fails():
    class Context:
        closed = False

        async def new_page(self):
            return object()

        async def close(self):
            self.closed = True
            raise RuntimeError("context close failed")

    class Browser:
        closed = False

        def __init__(self, context):
            self.context = context

        async def new_context(self, **kwargs):
            return self.context

        async def close(self):
            self.closed = True

    class Chromium:
        def __init__(self, browser):
            self.browser = browser

        async def launch(self, **kwargs):
            return self.browser

    context = Context()
    browser = Browser(context)
    playwright = type("Playwright", (), {"chromium": Chromium(browser)})()

    with pytest.raises(RuntimeError, match="context close failed"):
        async with _browser_page(playwright, headless=True):
            pass

    assert context.closed is True
    assert browser.closed is True


def test_report_normalizes_malformed_provider_fields():
    report = RunExecutor._report(
        json.dumps(
            {
                "verdict": "passed",
                "summary": {"unexpected": True},
                "findings": [
                    {"severity": "high", "title": "Broken", "detail": "Button"},
                    {"severity": "low", "title": 3, "detail": "invalid"},
                    "invalid",
                ],
                "recommendations": ["Fix the button", 4, {"bad": "shape"}],
            }
        ),
        "inspect page",
        "safe observation",
    )

    assert report == {
        "verdict": "passed",
        "summary": "safe observation",
        "findings": [{"severity": "high", "title": "Broken", "detail": "Button"}],
        "recommendations": ["Fix the button"],
        "plan": "inspect page",
    }
