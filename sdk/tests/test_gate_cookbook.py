"""Gate HITL cookbook — stream_hitl_until_end_turn unit tests."""

from __future__ import annotations

from typing import Any
from unittest.mock import AsyncMock, MagicMock

import pytest

from oma_sdk.cookbook import (
    StreamConfig,
    custom_tool_event_id,
    stop_reason_event_ids,
    stream_hitl_until_end_turn,
)


def _custom_tool_use(tool_id: str, name: str, args: dict[str, Any]) -> dict:
    return {
        "seq": 10,
        "type": "agent.custom_tool_use",
        "data": {
            "type": "agent.custom_tool_use",
            "id": tool_id,
            "name": name,
            "input": args,
        },
    }


def _requires_action_idle(seq: int, event_ids: list[str]) -> dict[str, Any]:
    return {
        "seq": seq,
        "type": "session.status_idle",
        "data": {
            "type": "session.status_idle",
            "stop_reason": {
                "type": "requires_action",
                "event_ids": event_ids,
            },
        },
    }


def _end_turn_idle(seq: int) -> dict[str, Any]:
    return {
        "seq": seq,
        "type": "session.status_idle",
        "data": {
            "type": "session.status_idle",
            "stop_reason": {"type": "end_turn"},
        },
    }


async def _gate_hitl_stream(_session_id: str):
    yield _custom_tool_use(
        "ctu_1",
        "decide",
        {"receipt_id": "r01", "action": "approve", "reason": "under limit"},
    )
    yield _requires_action_idle(11, ["ctu_1"])
    yield _custom_tool_use(
        "ctu_2",
        "escalate",
        {"receipt_id": "r02", "question": "near threshold"},
    )
    yield _requires_action_idle(12, ["ctu_2"])
    yield _requires_action_idle(13, ["ctu_2"])
    yield _end_turn_idle(14)


@pytest.mark.asyncio
async def test_stream_hitl_one_reply_per_requires_action_idle() -> None:
    """Sliding window: respond to at most one pending id per idle event."""

    async def _stream(_session_id: str):
        yield _custom_tool_use(
            "ctu_a",
            "decide",
            {"receipt_id": "r01", "action": "approve", "reason": "ok"},
        )
        yield _custom_tool_use(
            "ctu_b",
            "decide",
            {"receipt_id": "r02", "action": "approve", "reason": "ok"},
        )
        yield _requires_action_idle(11, ["ctu_a", "ctu_b"])
        yield _requires_action_idle(12, ["ctu_b"])
        yield _end_turn_idle(13)

    client = MagicMock()
    client.events.stream = lambda *_a, **_k: _stream("sess_gate")
    client.events.send = AsyncMock()
    client.events.list = AsyncMock(return_value={"data": []})
    client.sessions.events.send = MagicMock()
    client._http.get = AsyncMock(
        return_value=MagicMock(
            raise_for_status=MagicMock(),
            json=MagicMock(return_value={"status": "idle"}),
        )
    )

    state = await stream_hitl_until_end_turn(
        client,
        "sess_gate",
        send_events=[{"type": "user.message", "content": []}],
        on_custom_tool=lambda *_a, **_k: {"recorded": True},
        config=StreamConfig(
            timeout_sec=5.0,
            stream_connect_delay=0.01,
            idle_poll_max_wait=1.0,
        ),
    )

    assert client.events.send.await_count == 2
    assert state["responded_ids"] == {"ctu_a", "ctu_b"}


@pytest.mark.asyncio
async def test_stream_hitl_responds_and_dedupes() -> None:
    """GT4/GT5: requires_action loop posts custom_tool_result once per id."""
    client = MagicMock()
    client.events.stream = lambda *_a, **_k: _gate_hitl_stream("sess_gate")
    client.events.send = AsyncMock()
    client.events.list = AsyncMock(return_value={"data": []})
    client.sessions.events.send = MagicMock()
    client._http.get = AsyncMock(
        return_value=MagicMock(
            raise_for_status=MagicMock(),
            json=MagicMock(return_value={"status": "idle"}),
        )
    )

    seen: list[str] = []

    def on_custom_tool(
        name: str,
        event_id: str,
        args: dict[str, Any],
    ) -> dict[str, Any]:
        seen.append(f"{name}:{event_id}:{args.get('receipt_id')}")
        if name == "decide":
            return {"recorded": True}
        return {"human_decision": "approve"}

    state = await stream_hitl_until_end_turn(
        client,
        "sess_gate",
        send_events=[{"type": "user.message", "content": []}],
        on_custom_tool=on_custom_tool,
        config=StreamConfig(
            timeout_sec=5.0,
            stream_connect_delay=0.01,
            idle_poll_max_wait=1.0,
        ),
    )

    assert client.events.send.await_count == 2
    assert len(state["responded_ids"]) == 2
    assert "ctu_1" in state["responded_ids"]
    assert "ctu_2" in state["responded_ids"]
    assert len(seen) == 2


def test_stop_reason_event_ids() -> None:
    payload = {
        "stop_reason": {
            "type": "requires_action",
            "event_ids": ["a", "b"],
        }
    }
    assert stop_reason_event_ids(payload) == ["a", "b"]


def test_custom_tool_event_id() -> None:
    ev = _custom_tool_use("ctu_x", "decide", {})
    assert custom_tool_event_id(ev) == "ctu_x"
