"""Unit tests for oma_sdk.cookbook helpers (no live server)."""

from __future__ import annotations

import pytest

from oma_sdk.cookbook import (
    event_payload,
    event_type,
    print_stream_event,
    stop_reason_type,
)


def test_event_type_top_level() -> None:
    assert event_type({"type": "agent.message"}) == "agent.message"


def test_event_type_nested_data() -> None:
    ev = {"data": {"type": "session.status_idle", "stop_reason": {"type": "end_turn"}}}
    assert event_type(ev) == "session.status_idle"


def test_event_payload_unwraps_data() -> None:
    inner = {"type": "agent.tool_use", "name": "bash"}
    assert event_payload({"data": inner}) == inner


def test_stop_reason_type_end_turn() -> None:
    payload = {"type": "session.status_idle", "stop_reason": {"type": "end_turn"}}
    assert stop_reason_type(payload) == "end_turn"


def test_stop_reason_type_requires_action_not_end_turn() -> None:
    payload = {
        "type": "session.status_idle",
        "stop_reason": {"type": "requires_action"},
    }
    assert stop_reason_type(payload) != "end_turn"


def test_print_stream_event_agent_message_preview(capsys: pytest.CaptureFixture[str]) -> None:
    ev = {
        "type": "agent.message",
        "content": [{"type": "text", "text": "hello world"}],
    }
    print_stream_event(ev, preview_length=5)
    assert capsys.readouterr().out.strip() == "hello..."


def test_print_stream_event_tool_use(capsys: pytest.CaptureFixture[str]) -> None:
    ev = {"type": "agent.tool_use", "name": "bash"}
    print_stream_event(ev)
    assert "[bash]" in capsys.readouterr().out
