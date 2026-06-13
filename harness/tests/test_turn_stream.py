import json

import pytest
from fastapi.testclient import TestClient

from oma_adapter.main import app
from oma_adapter.turn import run_turn_stream
from oma_adapter.types import AgentSnapshot


@pytest.mark.asyncio
async def test_run_turn_stream_emits_incrementally(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("OMA_FAKE_HARNESS", "1")
    emitted: list[dict] = []

    class FakeSession:
        async def prompt(self, _text: str) -> None:
            return None

        async def wait_for_idle(self) -> None:
            return None

        def get_last_assistant_text(self) -> str:
            return "streamed"

    class FakeResult:
        session = FakeSession()

    async def fake_create(_opts):  # noqa: ANN001
        return FakeResult()

    async def on_event(event: dict) -> None:
        emitted.append(event)

    resp = await run_turn_stream(
        session_id="sess_stream",
        agent=AgentSnapshot(id="a", name="n", model="faux/test"),
        events=[
            {
                "type": "user.message",
                "content": [{"type": "text", "text": "ping"}],
            }
        ],
        workdir="/tmp",
        create_session=fake_create,
        on_event=on_event,
    )
    assert resp.events
    assert emitted
    assert emitted[0]["type"] == "agent.message"


@pytest.mark.asyncio
async def test_run_turn_stream_with_outbound_proxy_addr(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("OMA_FAKE_HARNESS", "1")
    emitted: list[dict] = []

    class FakeSession:
        async def prompt(self, _text: str) -> None:
            return None

        async def wait_for_idle(self) -> None:
            return None

        def get_last_assistant_text(self) -> str:
            return "streamed"

    class FakeResult:
        session = FakeSession()

    async def fake_create(_opts):  # noqa: ANN001
        return FakeResult()

    async def on_event(event: dict) -> None:
        emitted.append(event)

    resp = await run_turn_stream(
        session_id="sess_stream",
        agent=AgentSnapshot(id="a", name="n", model="faux/test"),
        events=[
            {
                "type": "user.message",
                "content": [{"type": "text", "text": "ping"}],
            }
        ],
        workdir="/tmp",
        outbound_proxy_addr=":8790",
        outbound_proxy_api_key="dev-key",
        create_session=fake_create,
        on_event=on_event,
    )
    assert resp.events
    assert emitted
    assert emitted[0]["type"] == "agent.message"


async def _fake_run_turn_stream(**kwargs):  # noqa: ANN003
    on_event = kwargs["on_event"]
    event = {
        "type": "agent.message",
        "content": [{"type": "text", "text": "streamed"}],
    }
    await on_event(event)
    from oma_adapter.types import TurnResponse

    return TurnResponse(events=[event])


def test_internal_turn_stream_ndjson(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setattr("oma_adapter.main.run_turn_stream", _fake_run_turn_stream)
    client = TestClient(app)
    resp = client.post(
        "/internal/turn/stream",
        json={
            "session_id": "sess_http",
            "agent": {
                "id": "a1",
                "name": "agent",
                "model": "faux/test",
                "version": 1,
            },
            "events": [
                {
                    "type": "user.message",
                    "content": [{"type": "text", "text": "hello"}],
                }
            ],
            "workdir": "/tmp",
        },
    )
    assert resp.status_code == 200
    lines = [ln for ln in resp.text.splitlines() if ln.strip()]
    assert lines
    first = json.loads(lines[0])
    assert first["type"] == "agent.message"
