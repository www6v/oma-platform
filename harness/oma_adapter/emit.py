"""Map piPy agent events to OMA session events."""

from __future__ import annotations

import json
import uuid
from typing import Any


def emit_oma_events(
    raw_events: list[dict[str, Any]],
    *,
    seen_agent_text: set[str] | None = None,
) -> list[dict[str, Any]]:
    out: list[dict[str, Any]] = []
    seen_agent_text = seen_agent_text if seen_agent_text is not None else set()
    for item in raw_events:
        kind = item.get("type") or item.get("event")
        if kind in {"assistant_message", "agent.message"}:
            text = _extract_text(item)
            if text and text not in seen_agent_text:
                seen_agent_text.add(text)
                out.append(_agent_message(text))
        elif kind in {"message_end", "turn_end"}:
            message = item.get("message")
            if isinstance(message, dict) and message.get("role") == "assistant":
                text = _extract_pi_message_text(message)
                if text and text not in seen_agent_text:
                    seen_agent_text.add(text)
                    out.append(_agent_message(text))
        elif kind in {"tool_use", "agent.tool_use", "tool_execution_start"}:
            tool_id = (
                item.get("id")
                or item.get("toolCallId")
                or item.get("tool_use_id")
                or f"tool_{uuid.uuid4().hex[:12]}"
            )
            out.append(
                {
                    "type": "agent.tool_use",
                    "id": tool_id,
                    "name": item.get("toolName") or item.get("name", "tool"),
                    "input": item.get("args") or item.get("input") or {},
                }
            )
        elif kind in {"tool_result", "agent.tool_result", "tool_execution_end"}:
            out.append(
                {
                    "type": "agent.tool_result",
                    "tool_use_id": (
                        item.get("toolCallId")
                        or item.get("tool_use_id")
                        or item.get("id")
                        or ""
                    ),
                    "content": [
                        {
                            "type": "text",
                            "text": _stringify(
                                item.get("result") or item.get("content")
                            ),
                        }
                    ],
                }
            )
    return out


def _agent_message(text: str) -> dict[str, Any]:
    return {
        "type": "agent.message",
        "content": [{"type": "text", "text": text}],
    }


def _extract_pi_message_text(message: dict[str, Any]) -> str:
    content = message.get("content")
    if isinstance(content, str):
        return content.strip()
    if not isinstance(content, list):
        return ""
    parts: list[str] = []
    for block in content:
        if not isinstance(block, dict):
            continue
        if block.get("type") == "text" and block.get("text"):
            parts.append(str(block["text"]))
    return "".join(parts).strip()


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
    if isinstance(value, dict):
        text = _tool_result_text(value)
        if text:
            return text
        return json.dumps(value, ensure_ascii=False, default=str)
    if isinstance(value, list):
        return json.dumps(value, ensure_ascii=False, default=str)
    return str(value)


def _tool_result_text(payload: dict[str, Any]) -> str:
    """Extract readable text from piPy AgentToolResult-shaped dicts."""
    content = payload.get("content")
    if not isinstance(content, list):
        return ""
    parts: list[str] = []
    for block in content:
        if not isinstance(block, dict):
            continue
        if block.get("type") == "text" and block.get("text"):
            parts.append(str(block["text"]))
    return "".join(parts).strip()
