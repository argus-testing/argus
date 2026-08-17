# pyright: reportMissingImports=false
from __future__ import annotations

import asyncio
from collections import defaultdict
from collections.abc import AsyncIterator
from contextlib import asynccontextmanager, suppress
from pathlib import Path
from typing import Any, Protocol

from fastapi import FastAPI, HTTPException, WebSocket, WebSocketDisconnect
from fastapi.responses import FileResponse
from fastapi.staticfiles import StaticFiles
from pydantic import BaseModel, Field, field_validator

from .config import Settings
from .network import sanitize_url, validate_url
from .pipeline import RunExecutor
from .store import TERMINAL_STATUSES, RunStatus, RunStore

TERMINAL_EVENT_TYPES = {"run.completed", "run.failed", "run.cancelled"}


class Runner(Protocol):
    async def run(self, run_id: str) -> None: ...


class CreateRunRequest(BaseModel):
    url: str
    instructions: str = Field(min_length=1, max_length=10_000)

    @field_validator("url")
    @classmethod
    def valid_url(cls, value: str) -> str:
        return validate_url(value)


class EventHub:
    def __init__(self) -> None:
        self._subscribers: dict[str, set[asyncio.Queue[dict[str, Any]]]] = defaultdict(
            set
        )

    async def publish(self, run_id: str, event: dict[str, Any]) -> None:
        for queue in tuple(self._subscribers.get(run_id, ())):
            queue.put_nowait(event)

    @asynccontextmanager
    async def subscribe(
        self, run_id: str
    ) -> AsyncIterator[asyncio.Queue[dict[str, Any]]]:
        queue: asyncio.Queue[dict[str, Any]] = asyncio.Queue()
        self._subscribers[run_id].add(queue)
        try:
            yield queue
        finally:
            subscribers = self._subscribers.get(run_id)
            if subscribers is not None:
                subscribers.discard(queue)
                if not subscribers:
                    self._subscribers.pop(run_id, None)


def create_app(
    *, settings: Settings | None = None, runner: Runner | None = None
) -> FastAPI:
    resolved_settings = settings if settings is not None else Settings.from_env()
    store = RunStore(Path(resolved_settings.data_dir) / "argus.db")
    hub = EventHub()
    tasks: dict[str, asyncio.Task[None]] = {}

    async def emit(run_id: str, event_type: str, data: dict[str, Any]) -> None:
        event = await store.add_event(run_id, event_type, data)
        await hub.publish(run_id, event)

    async def publish(event: dict[str, Any]) -> None:
        await hub.publish(event["run_id"], event)

    active_runner = runner or RunExecutor(store, resolved_settings, emit, publish)

    def task_done(run_id: str, task: asyncio.Task[None]) -> None:
        if tasks.get(run_id) is task:
            tasks.pop(run_id, None)
        with suppress(asyncio.CancelledError):
            task.exception()

    @asynccontextmanager
    async def lifespan(_: FastAPI) -> AsyncIterator[None]:
        await store.initialize()
        for event in await store.reconcile_interrupted():
            await publish(event)
        yield
        running_tasks = tuple(tasks.values())
        for task in running_tasks:
            task.cancel()
        for task in running_tasks:
            with suppress(asyncio.CancelledError):
                await task

    app = FastAPI(title="Argus", version="0.1.0", lifespan=lifespan)
    app.state.store = store
    app.state.hub = hub
    app.state.tasks = tasks

    @app.post("/api/runs", status_code=201)
    async def create_run(request: CreateRunRequest) -> dict[str, Any]:
        run = await store.create_run(sanitize_url(request.url), request.instructions)
        await emit(run["id"], "run.queued", {})
        task = asyncio.create_task(active_runner.run(run["id"]))
        tasks[run["id"]] = task
        task.add_done_callback(lambda completed: task_done(run["id"], completed))
        return run

    @app.get("/api/runs")
    async def list_runs(limit: int = 100) -> list[dict[str, Any]]:
        return await store.list_runs(min(max(limit, 1), 500))

    @app.get("/api/runs/{run_id}")
    async def get_run(run_id: str) -> dict[str, Any]:
        run = await store.get_run(run_id, events=True)
        if run is None:
            raise HTTPException(404, "Run not found")
        return run

    @app.post("/api/runs/{run_id}/cancel")
    async def cancel_run(run_id: str) -> dict[str, Any]:
        run = await store.get_run(run_id)
        if run is None:
            raise HTTPException(404, "Run not found")
        if run["status"] not in TERMINAL_STATUSES:
            event = await store.transition(
                run_id,
                expected={RunStatus.QUEUED, RunStatus.RUNNING},
                status=RunStatus.CANCELLED,
                event_type="run.cancelled",
            )
            if event is not None:
                await publish(event)
                task = tasks.get(run_id)
                if task:
                    task.cancel()
        return await store.get_run(run_id)  # type: ignore[return-value]

    @app.get("/api/settings")
    async def get_settings() -> dict[str, Any]:
        return {
            "gemini_configured": bool(resolved_settings.gemini_api_key),
            "model": resolved_settings.gemini_model,
        }

    @app.websocket("/ws/runs/{run_id}")
    async def run_events(websocket: WebSocket, run_id: str) -> None:
        if await store.get_run(run_id) is None:
            await websocket.close(code=4404)
            return
        await websocket.accept()
        try:
            async with hub.subscribe(run_id) as queue:
                last_id = 0
                for event in await store.events_after(run_id):
                    await websocket.send_json(event)
                    last_id = event["id"]
                    if event["type"] in TERMINAL_EVENT_TYPES:
                        await websocket.close()
                        return
                run = await store.get_run(run_id)
                if run and run["status"] in TERMINAL_STATUSES:
                    for event in await store.events_after(run_id, last_id):
                        await websocket.send_json(event)
                        last_id = event["id"]
                    await websocket.close()
                    return
                while True:
                    event_task = asyncio.create_task(queue.get())
                    receive_task = asyncio.create_task(websocket.receive())
                    done, pending = await asyncio.wait(
                        {event_task, receive_task},
                        return_when=asyncio.FIRST_COMPLETED,
                    )
                    for task in pending:
                        task.cancel()
                        with suppress(asyncio.CancelledError):
                            await task
                    if receive_task in done:
                        message = receive_task.result()
                        if message["type"] == "websocket.disconnect":
                            return
                    if event_task not in done:
                        continue
                    event = event_task.result()
                    if event["id"] <= last_id:
                        continue
                    await websocket.send_json(event)
                    last_id = event["id"]
                    if event["type"] in TERMINAL_EVENT_TYPES:
                        await websocket.close()
                        return
        except (RuntimeError, WebSocketDisconnect):
            pass

    screenshots = resolved_settings.data_dir / "screenshots"
    screenshots.mkdir(parents=True, exist_ok=True)
    app.mount("/screenshots", StaticFiles(directory=screenshots), name="screenshots")

    frontend = Path(__file__).parent / "static"
    if frontend.exists():
        assets = frontend / "assets"
        if assets.exists():
            app.mount("/assets", StaticFiles(directory=assets), name="assets")

        @app.get("/{path:path}", include_in_schema=False)
        async def frontend_route(path: str) -> FileResponse:
            candidate = frontend / path
            if path and candidate.is_file() and frontend in candidate.resolve().parents:
                return FileResponse(candidate)
            return FileResponse(frontend / "index.html")

    return app


app = create_app()
