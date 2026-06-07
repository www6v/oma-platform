"""Map piPy agent events to OMA session events."""

from __future__ import annotations

from typing import Any


def emit_oma_events(raw_events: list[dict[str, Any]]) -> list[dict[str, Any]]:
    out: list[dict[str, Any]] = []
    for item in raw_events:
        kind = item.get("type") or item.get("event")
        if kind in {"assistant_message", "agent.message"}:
            text = _extract_text(item)
            if text:
                out.append(
                    {
                        "type": "agent.message",
                        "content": [{"type": "text", "text": text}],
                    }
                )
        elif kind in {"tool_use", "agent.tool_use"}:
            out.append(
                {
                    "type": "agent.tool_use",
                    "name": item.get("name", "tool"),
                    "input": item.get("input") or {},
                }
            )
        elif kind in {"tool_result", "agent.tool_result"}:
            out.append(
                {
                    "type": "agent.tool_result",
                    "tool_use_id": item.get("tool_use_id") or item.get("id") or "",
                    "content": [{"type": "text", "text": _stringify(item.get("content"))}],
                }
            )
    return out


def _extract_text(item: dict[str, Any]) -> str:
    if isinstance(item.get("text"), str):
        return item["text"]
    content = item.get("content")
    if isinstance(content, str):
        return content
    if isinstance(content, list):
        parts: list[str] = []
        for block in content:
            if isinstance(block, dict) and block.get("type") == "text":
                parts.append(str(block.get("text") or ""))
        return "".join(parts)
    return ""


def _stringify(value: Any) -> str:
    if value is None:
        return ""
    if isinstance(value, str):
        return value
    return str(value)
