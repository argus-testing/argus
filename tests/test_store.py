from __future__ import annotations

import asyncio

import pytest

from argus.store import RunStatus, RunStore


@pytest.mark.asyncio
async def test_competing_terminal_transitions_are_atomic(tmp_path):
    store = RunStore(tmp_path / "argus.db")
    await store.initialize()
    run = await store.create_run("https://example.com", "test")
    started = await store.transition(
        run["id"],
        expected={RunStatus.QUEUED},
        status=RunStatus.RUNNING,
        event_type="run.started",
    )
    assert started is not None

    completed, cancelled = await asyncio.gather(
        store.transition(
            run["id"],
            expected={RunStatus.RUNNING},
            status=RunStatus.PASSED,
            event_type="run.completed",
            event_data={"verdict": "passed"},
            report={"verdict": "passed"},
        ),
        store.transition(
            run["id"],
            expected={RunStatus.QUEUED, RunStatus.RUNNING},
            status=RunStatus.CANCELLED,
            event_type="run.cancelled",
        ),
    )

    assert sum(event is not None for event in (completed, cancelled)) == 1
    detail = await store.get_run(run["id"], events=True)
    assert detail is not None
    assert detail["status"] in {RunStatus.PASSED, RunStatus.CANCELLED}
    terminal_events = [
        event
        for event in detail["events"]
        if event["type"] in {"run.completed", "run.cancelled"}
    ]
    assert len(terminal_events) == 1
    assert (
        await store.transition(
            run["id"],
            expected={RunStatus.PASSED, RunStatus.CANCELLED},
            status=RunStatus.FAILED,
            event_type="run.failed",
        )
        is None
    )


@pytest.mark.asyncio
async def test_reconcile_interrupted_runs_fails_pending_runs_atomically(tmp_path):
    store = RunStore(tmp_path / "argus.db")
    await store.initialize()
    queued = await store.create_run("https://example.com/queued", "test")
    running = await store.create_run("https://example.com/running", "test")
    await store.transition(
        running["id"],
        expected={RunStatus.QUEUED},
        status=RunStatus.RUNNING,
        event_type="run.started",
    )

    events = await store.reconcile_interrupted()

    assert {event["run_id"] for event in events} == {queued["id"], running["id"]}
    for run_id in (queued["id"], running["id"]):
        detail = await store.get_run(run_id, events=True)
        assert detail is not None
        assert detail["status"] == RunStatus.FAILED
        assert detail["error"] == "Run interrupted by server restart"
        assert detail["events"][-1]["type"] == "run.failed"
        assert detail["events"][-1]["data"]["kind"] == "interrupted"
