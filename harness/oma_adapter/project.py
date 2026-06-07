"""Project OMA session events into piPy prompt input."""

from __future__ import annotations

from typing import Any


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


def project_oma_events(events: list[dict[str, Any]]) -> str:
    """Return the latest user message text for a stateless turn."""

    return latest_user_text(events)
