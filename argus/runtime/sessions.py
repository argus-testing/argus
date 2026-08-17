import asyncio
from collections.abc import Mapping
from dataclasses import dataclass, field
from typing import Protocol

from argus.runtime.messages import Message


@dataclass(slots=True)
class Session:
    session_id: str
    state: dict[str, object] = field(default_factory=dict)
    messages: tuple[Message, ...] = ()


class SessionStore(Protocol):
    async def get_or_create(
        self,
        session_id: str,
        state: Mapping[str, object] | None = None,
    ) -> Session: ...

    async def append(self, session_id: str, message: Message) -> None: ...


class InMemorySessionStore:
    def __init__(self) -> None:
        self._sessions: dict[str, Session] = {}
        self._lock = asyncio.Lock()

    async def get_or_create(
        self,
        session_id: str,
        state: Mapping[str, object] | None = None,
    ) -> Session:
        async with self._lock:
            if session_id not in self._sessions:
                self._sessions[session_id] = Session(
                    session_id=session_id,
                    state=dict(state or {}),
                )
            return self._sessions[session_id]

    async def append(self, session_id: str, message: Message) -> None:
        async with self._lock:
            session = self._sessions[session_id]
            session.messages += (message,)
