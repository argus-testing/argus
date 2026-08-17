# pyright: reportMissingImports=false
import asyncio
import json
from collections.abc import AsyncIterator, Awaitable, Callable, Mapping
from contextlib import asynccontextmanager
from typing import Any

from argus.runtime.events import CompletedEvent, ToolCallEvent, ToolResultEvent
from argus.runtime.messages import ImagePart, Message, MessageRole, TextPart
from argus.runtime.models import AgentSpec, ModelRef
from argus.runtime.runtime import AgentRuntime
from argus.runtime.sessions import InMemorySessionStore
from argus.runtime.tools import Tool

from .config import Settings
from .network import sanitize_url, validate_url
from .providers.gemini import GeminiProvider
from .store import RunStatus, RunStore

EventSink = Callable[[str, str, dict[str, Any]], Awaitable[None]]
EventPublisher = Callable[[dict[str, Any]], Awaitable[None]]


def _navigation_url(value: object) -> str:
    return validate_url(value)


def _tool_event_arguments(
    tool_name: str, arguments: Mapping[str, object]
) -> dict[str, object]:
    persisted = dict(arguments)
    if tool_name == "navigate":
        persisted["url"] = sanitize_url(_navigation_url(persisted.get("url")))
    elif tool_name == "type_text" and "text" in persisted:
        persisted["text"] = "[redacted]"
    return persisted


def _persisted_tool_result(tool_name: str, result: object) -> object:
    if tool_name == "inspect_page":
        return {
            "omitted": True,
            "summary": "Page inspection omitted from persisted events",
        }
    if isinstance(result, Mapping):
        persisted = dict(result)
        if isinstance(persisted.get("url"), str):
            persisted["url"] = sanitize_url(persisted["url"])
        return persisted
    return result


@asynccontextmanager
async def _browser_page(playwright: Any, *, headless: bool) -> AsyncIterator[Any]:
    browser = await playwright.chromium.launch(headless=headless)
    try:
        context = await browser.new_context(viewport={"width": 1440, "height": 900})
        try:
            page = await context.new_page()
            yield page
        finally:
            await context.close()
    finally:
        await browser.close()


async def _ignore_event(_: dict[str, Any]) -> None:
    pass


class RunExecutor:
    def __init__(
        self,
        store: RunStore,
        settings: Settings,
        emit: EventSink,
        publish: EventPublisher | None = None,
    ) -> None:
        self.store = store
        self.settings = settings
        self.emit = emit
        self.publish = publish or _ignore_event

    async def run(self, run_id: str) -> None:
        run = await self.store.get_run(run_id)
        if run is None:
            return
        if not self.settings.gemini_api_key:
            await self._fail(
                run_id, "GEMINI_API_KEY is not configured", "configuration"
            )
            return

        started = await self.store.transition(
            run_id,
            expected={RunStatus.QUEUED},
            status=RunStatus.RUNNING,
            event_type="run.started",
        )
        if started is None:
            return
        await self.publish(started)
        try:
            async with asyncio.timeout(self.settings.run_timeout_seconds):
                report = await self._execute(run_id, run)
            status = (
                RunStatus.PASSED if report["verdict"] == "passed" else RunStatus.FAILED
            )
            completed = await self.store.transition(
                run_id,
                expected={RunStatus.RUNNING},
                status=status,
                event_type="run.completed",
                event_data={"verdict": report["verdict"]},
                report=report,
            )
            if completed is not None:
                await self.publish(completed)
        except asyncio.CancelledError:
            cancelled = await self.store.transition(
                run_id,
                expected={RunStatus.QUEUED, RunStatus.RUNNING},
                status=RunStatus.CANCELLED,
                event_type="run.cancelled",
            )
            if cancelled is not None:
                await self.publish(cancelled)
            raise
        except TimeoutError:
            await self._fail(run_id, "Run timed out", "timeout")
        except Exception as exc:  # noqa: BLE001
            await self._fail(
                run_id, str(exc) or type(exc).__name__, self._error_kind(exc)
            )

    async def _execute(self, run_id: str, run: dict[str, Any]) -> dict[str, Any]:
        from playwright.async_api import async_playwright

        run["url"] = sanitize_url(validate_url(run["url"]))
        provider = GeminiProvider(api_key=self.settings.gemini_api_key or "")
        runtime = AgentRuntime(
            providers={"gemini": provider}, sessions=InMemorySessionStore()
        )
        model = ModelRef(provider="gemini", model=self.settings.gemini_model)
        plan = await self._complete(
            runtime,
            AgentSpec(
                name="planner",
                model=model,
                instruction="Plan a concise visual UI test. Return plain text steps. Never request or enter credentials.",
            ),
            f"Target: {run['url']}\nGoal: {run['instructions']}",
            f"{run_id}:plan",
        )
        await self.emit(run_id, "plan.completed", {"plan": plan})

        async with (
            async_playwright() as playwright,
            _browser_page(playwright, headless=self.settings.headless) as page,
        ):
            await page.goto(run["url"], wait_until="domcontentloaded", timeout=30_000)
            initial_path, initial_bytes = await self._capture(run_id, page, "initial")
            await self.emit(
                run_id,
                "browser.screenshot",
                {"path": initial_path, "label": "Initial page"},
            )

            async def inspect_page() -> dict[str, str]:
                """Inspect the current page title, URL, visible text, and interactive elements."""
                snapshot = await page.locator("body").inner_text(timeout=10_000)
                elements = await page.locator(
                    "a, button, input, select, textarea"
                ).all_inner_texts()
                return {
                    "url": sanitize_url(page.url),
                    "title": await page.title(),
                    "text": snapshot[:12000],
                    "interactive": " | ".join(elements)[:4000],
                }

            async def click(selector: str) -> dict[str, str]:
                """Click one element using a Playwright CSS or text selector."""
                await page.locator(selector).first.click(timeout=10_000)
                return {"url": sanitize_url(page.url), "result": "clicked"}

            async def type_text(selector: str, text: str) -> dict[str, str]:
                """Fill a non-secret text value into one element."""
                await page.locator(selector).first.fill(text, timeout=10_000)
                return {"result": "filled"}

            async def navigate(url: str) -> dict[str, str]:
                """Navigate to an HTTP(S) URL without embedded credentials."""
                target = validate_url(url)
                await page.goto(target, wait_until="domcontentloaded", timeout=30_000)
                return {"url": sanitize_url(page.url), "title": await page.title()}

            async def screenshot(label: str = "Evidence") -> dict[str, str]:
                """Capture the current page as report evidence."""
                path, _ = await self._capture(run_id, page, label)
                await self.emit(
                    run_id, "browser.screenshot", {"path": path, "label": label}
                )
                return {"path": path, "label": label}

            agent = AgentSpec(
                name="browser",
                model=model,
                instruction=(
                    "Execute the supplied visual test plan with the browser tools. Inspect before acting, "
                    "capture evidence, do not enter credentials or destructive data, and summarize observations."
                ),
                tools=tuple(
                    Tool.from_callable(tool)
                    for tool in (inspect_page, click, type_text, navigate, screenshot)
                ),
            )
            message = Message(
                role=MessageRole.USER,
                parts=(
                    TextPart(
                        text=f"Target: {run['url']}\nGoal: {run['instructions']}\nPlan:\n{plan}"
                    ),
                    ImagePart(data=initial_bytes, media_type="image/png"),
                ),
            )
            observations = ""
            async for event in runtime.run(
                agent, session_id=f"{run_id}:browser", message=message
            ):
                if isinstance(event, ToolCallEvent):
                    await self.emit(
                        run_id,
                        "browser.action",
                        {
                            "tool": event.call.name,
                            "arguments": _tool_event_arguments(
                                event.call.name, event.call.arguments
                            ),
                        },
                    )
                elif isinstance(event, ToolResultEvent):
                    await self.emit(
                        run_id,
                        "browser.observation",
                        {
                            "tool": event.result.name,
                            "result": _persisted_tool_result(
                                event.result.name, event.result.result
                            ),
                        },
                    )
                elif isinstance(event, CompletedEvent):
                    observations = "".join(
                        part.text
                        for part in event.message.parts
                        if isinstance(part, TextPart)
                    )
            final_path, final_bytes = await self._capture(run_id, page, "final")
            await self.emit(
                run_id,
                "browser.screenshot",
                {"path": final_path, "label": "Final page"},
            )

        report_text = await self._complete_message(
            runtime,
            AgentSpec(
                name="reporter",
                model=model,
                instruction=(
                    "Return only JSON with keys verdict (passed, failed, or inconclusive), summary, "
                    "findings (array of objects with severity, title, detail), and recommendations (array of strings)."
                ),
            ),
            Message(
                role=MessageRole.USER,
                parts=(
                    TextPart(
                        text=f"Goal: {run['instructions']}\nPlan: {plan}\nObservations: {observations}"
                    ),
                    ImagePart(data=final_bytes, media_type="image/png"),
                ),
            ),
            f"{run_id}:report",
        )
        return self._report(report_text, plan, observations)

    async def _capture(self, run_id: str, page: Any, label: str) -> tuple[str, bytes]:
        directory = self.settings.data_dir / "screenshots" / run_id
        directory.mkdir(parents=True, exist_ok=True)
        safe_label = (
            "".join(
                character if character.isalnum() else "-" for character in label.lower()
            ).strip("-")
            or "evidence"
        )
        filename = f"{safe_label}-{len(list(directory.glob('*.png'))) + 1}.png"
        path = directory / filename
        data = await page.screenshot(path=str(path), full_page=True)
        public_path = f"/screenshots/{run_id}/{filename}"
        await self.store.add_screenshot(run_id, public_path)
        return public_path, data

    @staticmethod
    async def _complete(
        runtime: AgentRuntime, agent: AgentSpec, text: str, session_id: str
    ) -> str:
        return await RunExecutor._complete_message(
            runtime,
            agent,
            Message(role=MessageRole.USER, parts=(TextPart(text=text),)),
            session_id,
        )

    @staticmethod
    async def _complete_message(
        runtime: AgentRuntime, agent: AgentSpec, message: Message, session_id: str
    ) -> str:
        output = ""
        async for event in runtime.run(agent, session_id=session_id, message=message):
            if isinstance(event, CompletedEvent):
                output = "".join(
                    part.text
                    for part in event.message.parts
                    if isinstance(part, TextPart)
                )
        return output

    @staticmethod
    def _report(text: str, plan: str, observations: str) -> dict[str, Any]:
        try:
            cleaned = text.strip().removeprefix("```json").removesuffix("```").strip()
            decoded = json.loads(cleaned)
            if not isinstance(decoded, dict) or decoded.get("verdict") not in {
                "passed",
                "failed",
                "inconclusive",
            }:
                raise ValueError
            findings = decoded.get("findings")
            recommendations = decoded.get("recommendations")
            report = {
                "verdict": decoded["verdict"],
                "summary": (
                    decoded["summary"]
                    if isinstance(decoded.get("summary"), str)
                    else observations
                ),
                "findings": [
                    {
                        "severity": finding["severity"],
                        "title": finding["title"],
                        "detail": finding["detail"],
                    }
                    for finding in findings
                    if isinstance(finding, dict)
                    and all(
                        isinstance(finding.get(field), str)
                        for field in ("severity", "title", "detail")
                    )
                ]
                if isinstance(findings, list)
                else [],
                "recommendations": [
                    recommendation
                    for recommendation in recommendations
                    if isinstance(recommendation, str)
                ]
                if isinstance(recommendations, list)
                else [],
            }
        except (json.JSONDecodeError, ValueError):
            report = {
                "verdict": "inconclusive",
                "summary": text or observations,
                "findings": [],
                "recommendations": [],
            }
        report["plan"] = plan
        return report

    async def _fail(self, run_id: str, message: str, kind: str) -> None:
        failed = await self.store.transition(
            run_id,
            expected={RunStatus.QUEUED, RunStatus.RUNNING},
            status=RunStatus.FAILED,
            event_type="run.failed",
            event_data={"kind": kind, "message": message},
            error=message,
        )
        if failed is not None:
            await self.publish(failed)

    @staticmethod
    def _error_kind(exc: Exception) -> str:
        name = f"{type(exc).__module__}.{type(exc).__name__}".lower()
        if "rate" in name:
            return "rate_limit"
        if "playwright" in name or "browser" in name:
            return "browser"
        if "provider" in name or "http" in name:
            return "provider"
        return "execution"
