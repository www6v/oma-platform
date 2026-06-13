"""Project OMA session events into piPy prompt input."""

from __future__ import annotations

from typing import Any

from oma_adapter.compaction import latest_compaction_boundary


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


def _summary_text(boundary: dict[str, Any]) -> str:
    parts: list[str] = []
    for block in boundary.get("summary") or []:
        if block.get("type") == "text" and block.get("text"):
            parts.append(str(block["text"]))
    return "\n".join(parts).strip()


def project_oma_events(events: list[dict[str, Any]]) -> str:
    """Return prompt text for a stateless turn."""

    user_text = latest_user_text(events)
    if not user_text:
        return ""

    boundary = latest_compaction_boundary(events)
    if boundary is None:
        return user_text

    summary = _summary_text(boundary)
    if not summary:
        return user_text

    return (
        "<conversation-summary>\n"
        f"{summary}\n"
        "</conversation-summary>\n\n"
        f"{user_text}"
    )
