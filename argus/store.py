import asyncio
import json
import sqlite3
from datetime import UTC, datetime
from enum import StrEnum
from pathlib import Path
from typing import Any
from uuid import uuid4


class RunStatus(StrEnum):
    QUEUED = "queued"
    RUNNING = "running"
    PASSED = "passed"
    FAILED = "failed"
    CANCELLED = "cancelled"


TERMINAL_STATUSES = {RunStatus.PASSED, RunStatus.FAILED, RunStatus.CANCELLED}


def _now() -> str:
    return datetime.now(UTC).isoformat()


class RunStore:
    def __init__(self, database: Path) -> None:
        database.parent.mkdir(parents=True, exist_ok=True)
        self.database = database
        self._lock = asyncio.Lock()

    async def initialize(self) -> None:
        async with self._lock:
            with self._connect() as connection:
                connection.executescript(
                    """
                    CREATE TABLE IF NOT EXISTS runs (
                        id TEXT PRIMARY KEY, url TEXT NOT NULL, instructions TEXT NOT NULL,
                        status TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL,
                        report_json TEXT, error TEXT
                    );
                    CREATE TABLE IF NOT EXISTS events (
                        id INTEGER PRIMARY KEY AUTOINCREMENT, run_id TEXT NOT NULL,
                        type TEXT NOT NULL, data_json TEXT NOT NULL, created_at TEXT NOT NULL,
                        FOREIGN KEY(run_id) REFERENCES runs(id) ON DELETE CASCADE
                    );
                    CREATE INDEX IF NOT EXISTS events_run_id_id ON events(run_id, id);
                    CREATE TABLE IF NOT EXISTS screenshots (
                        id TEXT PRIMARY KEY, run_id TEXT NOT NULL, path TEXT NOT NULL,
                        created_at TEXT NOT NULL,
                        FOREIGN KEY(run_id) REFERENCES runs(id) ON DELETE CASCADE
                    );
                    """
                )

    async def create_run(self, url: str, instructions: str) -> dict[str, Any]:
        run_id, now = uuid4().hex, _now()
        async with self._lock:
            with self._connect() as connection:
                connection.execute(
                    "INSERT INTO runs VALUES (?, ?, ?, ?, ?, ?, NULL, NULL)",
                    (run_id, url, instructions, RunStatus.QUEUED, now, now),
                )
        return await self.get_run(run_id)  # type: ignore[return-value]

    async def list_runs(self, limit: int = 100) -> list[dict[str, Any]]:
        async with self._lock:
            with self._connect() as connection:
                rows = connection.execute(
                    "SELECT * FROM runs ORDER BY created_at DESC LIMIT ?", (limit,)
                ).fetchall()
        return [self._run(row) for row in rows]

    async def get_run(
        self, run_id: str, *, events: bool = False
    ) -> dict[str, Any] | None:
        async with self._lock:
            with self._connect() as connection:
                row = connection.execute(
                    "SELECT * FROM runs WHERE id = ?", (run_id,)
                ).fetchone()
                event_rows = (
                    connection.execute(
                        "SELECT * FROM events WHERE run_id = ? ORDER BY id", (run_id,)
                    ).fetchall()
                    if row is not None and events
                    else []
                )
        if row is None:
            return None
        run = self._run(row)
        if events:
            run["events"] = [self._event(item) for item in event_rows]
        return run

    async def add_event(
        self, run_id: str, event_type: str, data: dict[str, Any] | None = None
    ) -> dict[str, Any]:
        now = _now()
        async with self._lock:
            with self._connect() as connection:
                cursor = connection.execute(
                    "INSERT INTO events(run_id, type, data_json, created_at) VALUES (?, ?, ?, ?)",
                    (run_id, event_type, json.dumps(data or {}), now),
                )
                event_id = cursor.lastrowid
        return {
            "id": event_id,
            "run_id": run_id,
            "type": event_type,
            "data": data or {},
            "created_at": now,
        }

    async def events_after(
        self, run_id: str, event_id: int = 0
    ) -> list[dict[str, Any]]:
        async with self._lock:
            with self._connect() as connection:
                rows = connection.execute(
                    "SELECT * FROM events WHERE run_id = ? AND id > ? ORDER BY id",
                    (run_id, event_id),
                ).fetchall()
        return [self._event(row) for row in rows]

    async def transition(
        self,
        run_id: str,
        *,
        expected: set[RunStatus],
        status: RunStatus,
        event_type: str,
        event_data: dict[str, Any] | None = None,
        report: dict[str, Any] | None = None,
        error: str | None = None,
    ) -> dict[str, Any] | None:
        now = _now()
        expected_values = tuple(expected)
        placeholders = ", ".join("?" for _ in expected_values)
        async with self._lock:
            with self._connect() as connection:
                connection.execute("BEGIN IMMEDIATE")
                cursor = connection.execute(
                    f"""
                    UPDATE runs
                    SET status = ?, updated_at = ?, report_json = COALESCE(?, report_json), error = ?
                    WHERE id = ? AND status IN ({placeholders})
                      AND status NOT IN (?, ?, ?)
                    """,
                    (
                        status,
                        now,
                        json.dumps(report) if report is not None else None,
                        error,
                        run_id,
                        *expected_values,
                        *TERMINAL_STATUSES,
                    ),
                )
                if cursor.rowcount != 1:
                    return None
                event_cursor = connection.execute(
                    "INSERT INTO events(run_id, type, data_json, created_at) VALUES (?, ?, ?, ?)",
                    (run_id, event_type, json.dumps(event_data or {}), now),
                )
        return {
            "id": event_cursor.lastrowid,
            "run_id": run_id,
            "type": event_type,
            "data": event_data or {},
            "created_at": now,
        }

    async def reconcile_interrupted(self) -> list[dict[str, Any]]:
        now = _now()
        data = {"kind": "interrupted", "message": "Run interrupted by server restart"}
        events: list[dict[str, Any]] = []
        async with self._lock:
            with self._connect() as connection:
                connection.execute("BEGIN IMMEDIATE")
                rows = connection.execute(
                    "SELECT id FROM runs WHERE status IN (?, ?)",
                    (RunStatus.QUEUED, RunStatus.RUNNING),
                ).fetchall()
                for row in rows:
                    cursor = connection.execute(
                        """
                        UPDATE runs SET status = ?, updated_at = ?, error = ?
                        WHERE id = ? AND status IN (?, ?)
                        """,
                        (
                            RunStatus.FAILED,
                            now,
                            data["message"],
                            row["id"],
                            RunStatus.QUEUED,
                            RunStatus.RUNNING,
                        ),
                    )
                    if cursor.rowcount != 1:
                        continue
                    event_cursor = connection.execute(
                        "INSERT INTO events(run_id, type, data_json, created_at) VALUES (?, ?, ?, ?)",
                        (row["id"], "run.failed", json.dumps(data), now),
                    )
                    events.append(
                        {
                            "id": event_cursor.lastrowid,
                            "run_id": row["id"],
                            "type": "run.failed",
                            "data": data,
                            "created_at": now,
                        }
                    )
        return events

    async def add_screenshot(self, run_id: str, path: str) -> str:
        screenshot_id = uuid4().hex
        async with self._lock:
            with self._connect() as connection:
                connection.execute(
                    "INSERT INTO screenshots VALUES (?, ?, ?, ?)",
                    (screenshot_id, run_id, path, _now()),
                )
        return screenshot_id

    def _connect(self) -> sqlite3.Connection:
        connection = sqlite3.connect(self.database)
        connection.row_factory = sqlite3.Row
        connection.execute("PRAGMA foreign_keys = ON")
        return connection

    @staticmethod
    def _run(row: sqlite3.Row) -> dict[str, Any]:
        return {
            "id": row["id"],
            "url": row["url"],
            "instructions": row["instructions"],
            "status": row["status"],
            "created_at": row["created_at"],
            "updated_at": row["updated_at"],
            "report": json.loads(row["report_json"]) if row["report_json"] else None,
            "error": row["error"],
        }

    @staticmethod
    def _event(row: sqlite3.Row) -> dict[str, Any]:
        return {
            "id": row["id"],
            "run_id": row["run_id"],
            "type": row["type"],
            "data": json.loads(row["data_json"]),
            "created_at": row["created_at"],
        }
