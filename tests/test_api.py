from __future__ import annotations

import asyncio
import time
from typing import Any, cast

import pytest
from fastapi.testclient import TestClient
from starlette.websockets import WebSocketDisconnect

from argus.app import create_app
from argus.config import Settings
from argus.store import RunStore


class WaitingRunner:
    async def run(self, run_id: str) -> None:
        await asyncio.sleep(60)


def make_client(tmp_path):
    settings = Settings(data_dir=tmp_path, gemini_api_key="test-key")
    app = create_app(settings=settings, runner=WaitingRunner())
    return TestClient(app)


def test_create_list_and_get_run(tmp_path):
    with make_client(tmp_path) as client:
        response = client.post(
            "/api/runs",
            json={"url": "https://example.com", "instructions": "Check navigation"},
        )
        assert response.status_code == 201
        created = response.json()
        assert created["status"] == "queued"
        assert created["url"] == "https://example.com"

        listed = client.get("/api/runs").json()
        assert [run["id"] for run in listed] == [created["id"]]
        assert (
            client.get(f"/api/runs/{created['id']}").json()["instructions"]
            == "Check navigation"
        )


def test_rejects_unsafe_target_urls(tmp_path):
    with make_client(tmp_path) as client:
        for url in (
            "file:///etc/passwd",
            "javascript:alert(1)",
            "https://user:pass@example.com",
            "https://example.com/?access_token=secret",
            "https://example.com/?api_key=secret",
        ):
            response = client.post(
                "/api/runs", json={"url": url, "instructions": "test"}
            )
            assert response.status_code == 422


def test_cancel_run_persists_event(tmp_path):
    with make_client(tmp_path) as client:
        created = client.post(
            "/api/runs", json={"url": "https://example.com", "instructions": "test"}
        ).json()
        response = client.post(f"/api/runs/{created['id']}/cancel")
        assert response.status_code == 200
        detail = client.get(f"/api/runs/{created['id']}").json()
        assert detail["status"] == "cancelled"
        assert detail["events"][-1]["type"] == "run.cancelled"


def test_websocket_replays_persisted_events(tmp_path):
    with make_client(tmp_path) as client:
        created = client.post(
            "/api/runs", json={"url": "https://example.com", "instructions": "test"}
        ).json()
        with client.websocket_connect(f"/ws/runs/{created['id']}") as socket:
            event = socket.receive_json()
            assert event["type"] == "run.queued"
            assert event["run_id"] == created["id"]
        assert created["id"] not in cast(Any, client.app).state.hub._subscribers


def test_api_accepts_local_app_targets(tmp_path):
    with make_client(tmp_path) as client:
        response = client.post(
            "/api/runs",
            json={"url": "http://127.0.0.1:8000", "instructions": "test"},
        )
    assert response.status_code == 201


def test_startup_reconciles_orphaned_run(tmp_path):
    store = RunStore(tmp_path / "argus.db")

    async def seed():
        await store.initialize()
        return await store.create_run("https://example.com", "test")

    run = asyncio.run(seed())
    settings = Settings(data_dir=tmp_path, gemini_api_key="test-key")
    with TestClient(create_app(settings=settings, runner=WaitingRunner())) as client:
        detail = client.get(f"/api/runs/{run['id']}").json()

    assert detail["status"] == "failed"
    assert detail["events"][-1]["type"] == "run.failed"
    assert detail["events"][-1]["data"]["kind"] == "interrupted"


def test_websocket_replay_closes_after_terminal_event(tmp_path):
    with make_client(tmp_path) as client:
        created = client.post(
            "/api/runs", json={"url": "https://example.com", "instructions": "test"}
        ).json()
        with client.websocket_connect(f"/ws/runs/{created['id']}") as socket:
            assert socket.receive_json()["type"] == "run.queued"
            client.post(f"/api/runs/{created['id']}/cancel")
            assert socket.receive_json()["type"] == "run.cancelled"
            with pytest.raises(WebSocketDisconnect):
                socket.receive_json()


def test_completed_tasks_are_removed_from_registry(tmp_path):
    class FinishedRunner:
        async def run(self, run_id):
            return None

    settings = Settings(data_dir=tmp_path, gemini_api_key="test-key")
    app = create_app(settings=settings, runner=FinishedRunner())
    with TestClient(app) as client:
        client.post(
            "/api/runs", json={"url": "https://example.com", "instructions": "test"}
        )
        for _ in range(20):
            if not app.state.tasks:
                break
            time.sleep(0.01)
        assert app.state.tasks == {}


def test_settings_only_exposes_key_presence(tmp_path):
    with make_client(tmp_path) as client:
        assert client.get("/api/settings").json() == {
            "gemini_configured": True,
            "model": "gemini-2.5-flash",
        }
