import asyncio

from argus.runtime.messages import Message, MessageRole, TextPart
from argus.runtime.sessions import InMemorySessionStore


def test_session_store_persists_messages_between_turns() -> None:
    async def exercise() -> None:
        store = InMemorySessionStore()
        session = await store.get_or_create("run-1", state={"run_id": "run-1"})
        message = Message(
            role=MessageRole.USER,
            parts=(TextPart(text="Test checkout"),),
        )

        await store.append("run-1", message)
        restored = await store.get_or_create("run-1")

        assert restored is session
        assert restored.messages == (message,)
        assert restored.state == {"run_id": "run-1"}

    asyncio.run(exercise())
