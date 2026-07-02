"""Project OMA session events into piPy prompt input."""

from __future__ import annotations

from typing import Any

from oma_adapter.compaction import (
    events_to_conversation_text,
    latest_compaction_boundary,
)

PRIMARY_THREAD_ID = "sthr_primary"

# Session frames excluded from harness conversation projection.
_NON_MODEL_EVENT_TYPES = frozenset(
    {
        "session.lifecycle",
        "session.status_running",
        "session.status_idle",
        "session.status_terminated",
        "session.error",
        "session.warning",
        "system.user_message_pending",
        "system.user_message_promoted",
        "system.user_message_cancelled",
        "span.model_request_start",
        "span.model_request_end",
        "span.model_first_token",
    }
)


def event_thread_id(event: dict[str, Any]) -> str:
    if not isinstance(event, dict):
        return PRIMARY_THREAD_ID
    tid = event.get("session_thread_id")
    if isinstance(tid, str) and tid.strip():
        return tid.strip()
    return PRIMARY_THREAD_ID


def filter_events_for_thread(
    events: list[dict[str, Any]],
    session_thread_id: str | None,
) -> list[dict[str, Any]]:
    if not session_thread_id or session_thread_id == PRIMARY_THREAD_ID:
        return [
            ev
            for ev in events
            if event_thread_id(ev) == PRIMARY_THREAD_ID
        ]
    return [
        ev
        for ev in events
        if event_thread_id(ev) == session_thread_id
    ]


def latest_user_text(
    events: list[dict[str, Any]],
    *,
    session_thread_id: str | None = None,
) -> str:
    scoped = filter_events_for_thread(events, session_thread_id)
    for event in reversed(scoped):
        if not isinstance(event, dict):
            continue
        if event.get("type") != "user.message":
            continue
        parts: list[str] = []
        for block in event.get("content") or []:
            if not isinstance(block, dict):
                continue
            if block.get("type") == "text" and block.get("text"):
                parts.append(str(block["text"]))
        if parts:
            return "\n".join(parts)
    return ""


def _last_user_message_index(events: list[dict[str, Any]]) -> int:
    for index in range(len(events) - 1, -1, -1):
        event = events[index]
        if not isinstance(event, dict):
            continue
        if event.get("type") != "user.message":
            continue
        for block in event.get("content") or []:
            if not isinstance(block, dict):
                continue
            if block.get("type") == "text" and block.get("text"):
                return index
    return -1


def _summary_text(boundary: dict[str, Any]) -> str:
    parts: list[str] = []
    for block in boundary.get("summary") or []:
        if not isinstance(block, dict):
            continue
        if block.get("type") == "text" and block.get("text"):
            parts.append(str(block["text"]))
    return "\n".join(parts).strip()


def _history_slice(events: list[dict[str, Any]], end_index: int) -> list[dict[str, Any]]:
    """Events before the latest user turn that should appear in the prompt."""

    if end_index <= 0:
        return []

    boundary = latest_compaction_boundary(events[:end_index])
    if boundary is None:
        return events[:end_index]

    summary = _summary_text(boundary)
    if not summary:
        return events[:end_index]

    boundary_index = -1
    for index, event in enumerate(events[:end_index]):
        if event is boundary:
            boundary_index = index
            break
    if boundary_index < 0:
        for index, event in enumerate(events[:end_index]):
            if event.get("type") != "agent.thread_context_compacted":
                continue
            if _summary_text(event) == summary:
                boundary_index = index

    if boundary_index < 0:
        return events[:end_index]

    return events[boundary_index + 1 : end_index]


def _continuation_events_after_user(
    events: list[dict[str, Any]],
    user_index: int,
) -> list[dict[str, Any]]:
    """Model-context events after the latest user.message (HITL resume tail)."""
    if user_index < 0 or user_index >= len(events) - 1:
        return []
    from oma_adapter.compaction import model_context_events

    tail = [
        ev
        for ev in events[user_index + 1 :]
        if isinstance(ev, dict)
        and ev.get("type") not in _NON_MODEL_EVENT_TYPES
        and ev.get("type") != "user.custom_tool_result"
    ]
    return model_context_events(tail)


def project_oma_events(
    events: list[dict[str, Any]],
    *,
    session_thread_id: str | None = None,
) -> str:
    """Return prompt text for a stateless turn."""

    scoped = filter_events_for_thread(events, session_thread_id)
    user_text = latest_user_text(
        scoped,
        session_thread_id=session_thread_id,
    )
    if not user_text:
        return ""

    last_user_index = _last_user_message_index(scoped)
    if last_user_index < 0:
        return user_text

    parts: list[str] = []
    history_events = _history_slice(scoped, last_user_index)
    history_events.extend(
        _continuation_events_after_user(scoped, last_user_index)
    )
    boundary = latest_compaction_boundary(scoped[:last_user_index])
    if boundary is not None:
        summary = _summary_text(boundary)
        if summary:
            parts.append(
                "<conversation-summary>\n"
                f"{summary}\n"
                "</conversation-summary>"
            )

    history_text = events_to_conversation_text(history_events)
    if history_text:
        parts.append(
            "<conversation-history>\n"
            f"{history_text}\n"
            "</conversation-history>"
        )

    parts.append(user_text)
    if len(parts) == 1:
        return user_text
    return "\n\n".join(parts)
