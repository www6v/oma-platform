"""Tests for context compaction."""

from __future__ import annotations

from oma_adapter.compaction import (
    estimate_events_tokens,
    latest_compaction_boundary,
    should_compact,
)
from oma_adapter.project import project_oma_events


def _long_events(count: int) -> list[dict]:
    events: list[dict] = []
    for i in range(count):
        events.append(
            {
                "type": "user.message" if i % 2 == 0 else "agent.message",
                "content": [
                    {
                        "type": "text",
                        "text": f"message {i} " + ("x" * 400),
                    }
                ],
            }
        )
    return events


def test_should_compact_respects_threshold() -> None:
    short = _long_events(2)
    assert should_compact(short, context_window_tokens=10_000) is False

    long = _long_events(20)
    assert should_compact(long, context_window_tokens=1_000) is True


def test_latest_compaction_boundary_picks_last_summary() -> None:
    events = [
        {
            "type": "agent.thread_context_compacted",
            "summary": [{"type": "text", "text": "first"}],
        },
        {
            "type": "agent.thread_context_compacted",
            "summary": [{"type": "text", "text": "second"}],
        },
    ]
    boundary = latest_compaction_boundary(events)
    assert boundary is not None
    assert boundary["summary"][0]["text"] == "second"


def test_project_oma_events_includes_summary() -> None:
    events = [
        {
            "type": "agent.thread_context_compacted",
            "summary": [{"type": "text", "text": "prior work"}],
        },
        {
            "type": "user.message",
            "content": [{"type": "text", "text": "continue"}],
        },
    ]
    prompt = project_oma_events(events)
    assert "<conversation-summary>" in prompt
    assert "prior work" in prompt
    assert prompt.endswith("continue")


def test_estimate_events_tokens_positive() -> None:
    events = _long_events(3)
    assert estimate_events_tokens(events) > 0
