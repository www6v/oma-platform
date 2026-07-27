"""Assemble assistant text from OMA session wire events.

``emit_oma_events`` streams replies as many contiguous ``agent.message``
events (each a text delta). Callers that need the full step/sub-agent
result must join those deltas — taking only the last event returns a
trailing fragment (seen in workflow traces as ``\"25\"`` / ``\"```\"``).
"""

from __future__ import annotations

from typing import Any

_SEGMENT_BREAK_TYPES = frozenset(
    {
        "agent.tool_use",
        "agent.tool_result",
        "agent.custom_tool_use",
        "user.message",
    }
)


def _text_from_agent_message(event: dict[str, Any]) -> str:
    parts: list[str] = []
    for block in event.get("content") or []:
        if not isinstance(block, dict):
            continue
        if block.get("type") == "text" and block.get("text"):
            parts.append(str(block["text"]))
    return "".join(parts)


def assemble_assistant_text(events: list[dict[str, Any]]) -> str:
    """Join streamed ``agent.message`` deltas; return the last segment."""
    segments: list[str] = []
    current: list[str] = []

    def flush() -> None:
        if current:
            segments.append("".join(current))
            current.clear()

    for event in events:
        if not isinstance(event, dict):
            continue
        etype = event.get("type")
        if etype == "agent.message":
            piece = _text_from_agent_message(event)
            if piece:
                current.append(piece)
            continue
        if etype in _SEGMENT_BREAK_TYPES:
            flush()

    flush()

    for text in reversed(segments):
        stripped = text.strip()
        if stripped:
            return stripped
    return ""
