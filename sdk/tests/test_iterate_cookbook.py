"""MT1 — multi-turn same-session tests for iterate cookbook parity."""

from __future__ import annotations

import asyncio
from typing import Any
from unittest.mock import AsyncMock, MagicMock

import pytest

from oma_sdk.cookbook import (
    StreamConfig,
    event_payload,
    event_type,
    stream_until_end_turn,
)


def _idle_end_turn(seq: int) -> dict[str, Any]:
    return {
        "seq": seq,
        "type": "session.status_idle",
        "data": {
            "type": "session.status_idle",
            "stop_reason": {"type": "end_turn"},
        },
    }


def _agent_message(seq: int, text: str) -> dict[str, Any]:
    return {
        "seq": seq,
        "type": "agent.message",
        "data": {
            "type": "agent.message",
            "content": [{"type": "text", "text": text}],
        },
    }


async def _two_turn_stream(session_id: str):
    """Simulate two turns: message → idle/end_turn × 2."""
    events = [
        _agent_message(1, "iterate-cookbook-turn-1-ok"),
        _idle_end_turn(2),
        _agent_message(3, "iterate-cookbook-turn-2-ok"),
        _idle_end_turn(4),
    ]
    for ev in events:
        yield ev


async def _two_turn_stream_live(session_id: str):
    """Replay turn 1 then live turn 2 (multi-turn SSE parity)."""
    for ev in (
        _agent_message(1, "iterate-cookbook-turn-1-ok"),
        _idle_end_turn(2),
    ):
        yield ev
    for ev in (
        _agent_message(3, "iterate-cookbook-turn-2-ok"),
        _idle_end_turn(4),
    ):
        yield ev


@pytest.mark.asyncio
async def test_multi_turn_stream_until_end_turn_twice() -> None:
    """MT1: second user.message + stream completes after first end_turn."""
    client = MagicMock()
    client.events.stream = lambda *_a, **_k: _two_turn_stream("sess_1")
    client.sessions.events.send = MagicMock()
    client.events.list = AsyncMock(
        side_effect=[
            {"data": []},
            {"data": [_idle_end_turn(2)]},
            {"data": [_idle_end_turn(4)]},
        ]
    )
    client._http.get = AsyncMock(
        return_value=MagicMock(
            raise_for_status=MagicMock(),
            json=MagicMock(return_value={"status": "idle"}),
        )
    )

    cfg = StreamConfig(timeout_sec=5.0, stream_connect_delay=0.0)

    await stream_until_end_turn(
        client,
        "sess_1",
        send_events=[
            {
                "type": "user.message",
                "content": [{"type": "text", "text": "fix calc.py"}],
            }
        ],
        config=cfg,
    )

    client.sessions.events.send.assert_called_once()
    client.sessions.events.send.reset_mock()

    await stream_until_end_turn(
        client,
        "sess_1",
        send_events=[
            {
                "type": "user.message",
                "content": [{"type": "text", "text": "verify calc.py"}],
            }
        ],
        config=cfg,
    )

    assert client.sessions.events.send.call_count == 1


@pytest.mark.asyncio
async def test_stream_ignores_requires_action_idle() -> None:
    """IF3: bare requires_action idle must not exit; end_turn must."""
    async def stream(_session_id: str, **_kwargs):
        yield _agent_message(1, "working")
        yield {
            "seq": 2,
            "type": "session.status_idle",
            "data": {
                "type": "session.status_idle",
                "stop_reason": {"type": "requires_action"},
            },
        }
        yield _agent_message(3, "done")
        yield _idle_end_turn(4)

    client = MagicMock()
    client.events.stream = stream
    client.events.list = AsyncMock(return_value={"data": []})
    client._http.get = AsyncMock(
        return_value=MagicMock(
            raise_for_status=MagicMock(),
            json=MagicMock(return_value={"status": "idle"}),
        )
    )

    await stream_until_end_turn(
        client,
        "sess_1",
        config=StreamConfig(timeout_sec=5.0, stream_connect_delay=0.0),
    )


def test_event_type_and_stop_reason_helpers() -> None:
    ev = _idle_end_turn(1)
    assert event_type(ev) == "session.status_idle"
    payload = event_payload(ev)
    assert payload["stop_reason"]["type"] == "end_turn"
