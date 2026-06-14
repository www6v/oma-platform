"""Project OMA session events into piPy prompt input."""

from __future__ import annotations

from typing import Any

from oma_adapter.compaction import (
    events_to_conversation_text,
    latest_compaction_boundary,
)


def latest_user_text(events: list[dict[str, Any]]) -> str:
    for event in reversed(events):
        if event.get("type") != "user.message":
            continue
        parts: list[str] = []
        for block in event.get("content") or []:
            if block.get("type") == "text" and block.get("text"):
                parts.append(str(block["text"]))
        if parts:
            return "\n".join(parts)
    return ""


def _last_user_message_index(events: list[dict[str, Any]]) -> int:
    for index in range(len(events) - 1, -1, -1):
        event = events[index]
        if event.get("type") != "user.message":
            continue
        for block in event.get("content") or []:
            if block.get("type") == "text" and block.get("text"):
                return index
    return -1


def _summary_text(boundary: dict[str, Any]) -> str:
    parts: list[str] = []
    for block in boundary.get("summary") or []:
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


def project_oma_events(events: list[dict[str, Any]]) -> str:
    """Return prompt text for a stateless turn."""

    user_text = latest_user_text(events)
    if not user_text:
        return ""

    last_user_index = _last_user_message_index(events)
    if last_user_index < 0:
        return user_text

    parts: list[str] = []
    history_events = _history_slice(events, last_user_index)
    boundary = latest_compaction_boundary(events[:last_user_index])
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
