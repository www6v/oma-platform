"""Map piPy agent events to OMA session events."""

from __future__ import annotations

import json
import uuid
from typing import Any

from oma_adapter.custom_tools import wire_tool_use_type


def _thread_sent_event_id(tool_call_id: str) -> str:
    return f"sevt-thread-sent-{tool_call_id}"


def _call_agent_tool_name(name: str | None) -> bool:
    return bool(name and name.startswith("call_agent_"))


def emit_oma_events(
    raw_events: list[dict[str, Any]],
    *,
    seen_agent_text: set[str] | None = None,
    custom_tool_names: frozenset[str] | None = None,
    event_lookup_buffer: list[dict[str, Any]] | None = None,
    streaming_state: dict[str, Any] | None = None,
) -> list[dict[str, Any]]:
    out: list[dict[str, Any]] = []
    seen_agent_text = seen_agent_text if seen_agent_text is not None else set()
    # streaming_state tracks the last emitted text length for message_update
    # deltas so the frontend sees incremental text instead of each update
    # repeating the entire accumulated message.
    if streaming_state is None:
        streaming_state = {"last_emitted_len": 0}
    lookup_events = (
        event_lookup_buffer if event_lookup_buffer is not None else raw_events
    )
    lookup_offset = (
        len(lookup_events) - len(raw_events)
        if event_lookup_buffer is not None
        else 0
    )
    for idx, item in enumerate(raw_events):
        # Skip non-dict items (defensive: pi events should be dicts but may include strings)
        if not isinstance(item, dict):
            # Log unexpected types for debugging
            import sys
            print(f"DEBUG emit_oma_events: Skipping non-dict at index {idx}: type={type(item).__name__}, value={item!r}", file=sys.stderr, flush=True)
            continue
        kind = item.get("type") or item.get("event")
        if kind == "message_update":
            # Streaming text delta: message contains the accumulated
            # AssistantMessage so far. Emit only the new portion.
            message = item.get("message")
            if isinstance(message, dict):
                full_text = _extract_pi_message_text(message)
                last_len = int(streaming_state.get("last_emitted_len", 0))
                if len(full_text) > last_len:
                    delta = full_text[last_len:]
                    streaming_state["last_emitted_len"] = len(full_text)
                    if delta:
                        out.append(_agent_message(delta))
        elif kind in {"assistant_message", "agent.message"}:
            text = _extract_text(item)
            if text and text not in seen_agent_text:
                seen_agent_text.add(text)
                out.append(_agent_message(text))
        elif kind in {"message_start"}:
            # New message started — reset streaming delta tracker so the
            # first message_update of the new message emits from position 0.
            streaming_state["last_emitted_len"] = 0
        elif kind in {"message_end", "turn_end"}:
            message = item.get("message")
            if isinstance(message, dict) and message.get("role") == "assistant":
                text = _extract_pi_message_text(message)
                last_len = int(streaming_state.get("last_emitted_len", 0))
                if text and text in seen_agent_text:
                    # This exact text was already emitted (e.g. via an
                    # assistant_message event); skip to avoid duplication.
                    pass
                elif text and len(text) > last_len:
                    # Emit any trailing portion that didn't come through as
                    # a message_update (defensive — handles the case where
                    # the agent produced text without streaming updates, or
                    # where the final message has extra content beyond the
                    # last streamed update).
                    delta = text[last_len:]
                    streaming_state["last_emitted_len"] = len(text)
                    if delta:
                        out.append(_agent_message(delta))
                elif text and last_len == 0:
                    # No streaming happened; emit the full text once.
                    seen_agent_text.add(text)
                    out.append(_agent_message(text))
                if text:
                    seen_agent_text.add(text)
                usage_span = _model_usage_span(message)
                if usage_span is not None:
                    out.append(usage_span)
            # Reset streaming tracker at message boundary.
            streaming_state["last_emitted_len"] = 0
        elif kind in {"tool_use", "agent.tool_use", "tool_execution_start"}:
            tool_id = (
                item.get("id")
                or item.get("toolCallId")
                or item.get("tool_use_id")
                or f"tool_{uuid.uuid4().hex[:12]}"
            )
            tool_name = item.get("toolName") or item.get("name", "tool")
            if (
                custom_tool_names is not None
                and str(tool_name) in custom_tool_names
            ):
                wire_type = "agent.custom_tool_use"
            else:
                wire_type = wire_tool_use_type(str(tool_name))
            tool_input = item.get("args") or item.get("input") or {}
            out.append(
                {
                    "type": wire_type,
                    "id": tool_id,
                    "name": tool_name,
                    "input": tool_input,
                }
            )
            if _call_agent_tool_name(str(tool_name)):
                message = tool_input.get("message") or tool_input.get("task") or ""
                out.append(
                    {
                        "type": "agent.thread_message_sent",
                        "id": _thread_sent_event_id(str(tool_id)),
                        "to_thread_id": str(tool_id),
                        "content": [{"type": "text", "text": str(message)}],
                    }
                )
        elif kind in {"tool_result", "agent.tool_result", "tool_execution_end"}:
            tool_use_id = (
                item.get("toolCallId")
                or item.get("tool_use_id")
                or item.get("id")
                or ""
            )
            tool_name = _tool_name_for_call(
                lookup_events,
                tool_use_id,
                lookup_offset + idx,
            )
            if (
                custom_tool_names is not None
                and tool_name is not None
                and tool_name in custom_tool_names
            ):
                continue
            result_text = _stringify(item.get("result") or item.get("content"))
            out.append(
                {
                    "type": "agent.tool_result",
                    "tool_use_id": tool_use_id,
                    "content": [
                        {
                            "type": "text",
                            "text": result_text,
                        }
                    ],
                }
            )
            if _call_agent_tool_name(tool_name):
                received: dict[str, Any] = {
                    "type": "agent.thread_message_received",
                    "from_thread_id": tool_use_id,
                    "content": [{"type": "text", "text": result_text}],
                    "parent_event_id": _thread_sent_event_id(tool_use_id),
                }
                if tool_name is not None:
                    agent_id = tool_name[len("call_agent_") :]
                    if agent_id:
                        received["from_agent_id"] = agent_id
                out.append(received)
    return out


def _tool_name_for_call(
    raw_events: list[dict[str, Any]],
    tool_use_id: str,
    end_index: int,
) -> str | None:
    """Resolve tool name from a preceding tool_use / tool_execution_start."""
    if not tool_use_id:
        return None
    for item in reversed(raw_events[: end_index + 1]):
        if not isinstance(item, dict):
            continue
        kind = item.get("type") or item.get("event")
        if kind not in {
            "tool_use",
            "agent.tool_use",
            "tool_execution_start",
        }:
            continue
        call_id = (
            item.get("toolCallId")
            or item.get("tool_use_id")
            or item.get("id")
        )
        if call_id != tool_use_id:
            continue
        name = item.get("toolName") or item.get("name")
        if isinstance(name, str) and name:
            return name
    return None


def _agent_message(text: str) -> dict[str, Any]:
    return {
        "type": "agent.message",
        "content": [{"type": "text", "text": text}],
    }


def _model_usage_span(message: dict[str, Any]) -> dict[str, Any] | None:
    usage = message.get("usage")
    if not isinstance(usage, dict):
        return None
    model_usage = {
        "input_tokens": _usage_int(usage, "input_tokens", "input"),
        "output_tokens": _usage_int(usage, "output_tokens", "output"),
        "cache_read_input_tokens": _usage_int(
            usage, "cache_read_input_tokens", "cache_read",
        ),
        "cache_creation_input_tokens": _usage_int(
            usage, "cache_creation_input_tokens", "cache_creation",
        ),
    }
    if all(v == 0 for v in model_usage.values()):
        return None
    return {
        "type": "span.model_request_end",
        "model_usage": model_usage,
    }


def _usage_int(usage: dict[str, Any], *keys: str) -> int:
    for key in keys:
        value = usage.get(key)
        if isinstance(value, bool):
            continue
        if isinstance(value, (int, float)):
            return int(value)
    return 0


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
