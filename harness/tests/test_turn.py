import pytest

from oma_adapter.emit import emit_oma_events
from oma_adapter.turn import run_turn
from oma_adapter.types import AgentSnapshot


def test_emit_agent_message() -> None:
    raw = [{"type": "assistant_message", "text": "hi"}]
    out = emit_oma_events(raw)
    assert out[0]["type"] == "agent.message"
    assert out[0]["content"][0]["text"] == "hi"


@pytest.mark.asyncio
async def test_run_turn_fake(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("OMA_FAKE_HARNESS", "1")

    class FakeSession:
        async def prompt(self, _text: str) -> None:
            return None

        async def wait_for_idle(self) -> None:
            return None

        def get_last_assistant_text(self) -> str:
            return "pong"

    class FakeResult:
        session = FakeSession()

    async def fake_create(_opts):  # noqa: ANN001
        return FakeResult()

    resp = await run_turn(
        session_id="sess_test",
        agent=AgentSnapshot(id="a", name="n", model="faux/test"),
        events=[
            {
                "type": "user.message",
                "content": [{"type": "text", "text": "ping"}],
            }
        ],
        workdir="/tmp",
        create_session=fake_create,
    )
    assert resp.events
    assert resp.events[0]["type"] == "agent.message"
