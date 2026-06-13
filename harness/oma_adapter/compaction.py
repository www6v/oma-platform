"""Context compaction before harness turns (OMA compaction.ts parity)."""

from __future__ import annotations

import time
from typing import Any

from oma_adapter.types import ModelConfig

TRIGGER_FRACTION = 0.75
DEFAULT_CONTEXT_WINDOW = 200_000

DEFAULT_SUMMARIZE_PROMPT = (
    "Summarize the entire conversation above. Preserve key decisions, file "
    "paths, tool results (commands run + their output), in-flight tasks, "
    "and explicit Next Steps. If the conversation already contains a "
    "<conversation-summary> block, produce an updated summary that "
    "supersedes it (combining prior summary + new activity). Be concise but "
    "specific. Output only the summary text, no preamble."
)


def estimate_event_tokens(event: dict[str, Any]) -> int:
    text = _event_text(event)
    if not text:
        return 0
    return max(1, (len(text) + 3) // 4)


def estimate_events_tokens(events: list[dict[str, Any]]) -> int:
    return sum(estimate_event_tokens(ev) for ev in events)


def resolve_context_window_tokens(model_id: str | None) -> int:
    mid = (model_id or "").lower()
    if "opus-4-7" in mid or "opus-4-6" in mid or "sonnet-4-6" in mid:
        return 1_000_000
    if "minimax" in mid:
        return 1_000_000
    if "haiku" in mid:
        return 200_000
    return DEFAULT_CONTEXT_WINDOW


def should_compact(
    events: list[dict[str, Any]],
    *,
    context_window_tokens: int,
    trigger_fraction: float = TRIGGER_FRACTION,
) -> bool:
    if len(events) < 4:
        return False
    tokens = estimate_events_tokens(_model_context_events(events))
    return tokens > int(context_window_tokens * trigger_fraction)


def latest_compaction_boundary(
    events: list[dict[str, Any]],
) -> dict[str, Any] | None:
    boundary: dict[str, Any] | None = None
    for event in events:
        if event.get("type") != "agent.thread_context_compacted":
            continue
        summary = event.get("summary")
        if isinstance(summary, list) and summary:
            boundary = event
    return boundary


def _model_context_events(events: list[dict[str, Any]]) -> list[dict[str, Any]]:
    out: list[dict[str, Any]] = []
    for event in events:
        typ = event.get("type")
        if typ in {
            "user.message",
            "agent.message",
            "agent.tool_use",
            "agent.tool_result",
            "agent.custom_tool_use",
            "agent.custom_tool_result",
        }:
            out.append(event)
    return out


def _event_text(event: dict[str, Any]) -> str:
    parts: list[str] = []
    for block in event.get("content") or []:
        if not isinstance(block, dict):
            continue
        if block.get("type") == "text" and block.get("text"):
            parts.append(str(block["text"]))
    if event.get("type") == "agent.tool_use":
        name = event.get("name")
        if name:
            parts.append(str(name))
        inp = event.get("input")
        if inp is not None:
            parts.append(str(inp))
    return "\n".join(parts)


def events_to_conversation_text(events: list[dict[str, Any]]) -> str:
    lines: list[str] = []
    for event in _model_context_events(events):
        role = "user"
        if str(event.get("type", "")).startswith("agent."):
            role = "assistant"
        text = _event_text(event)
        if not text:
            continue
        lines.append(f"{role.upper()}: {text}")
    return "\n".join(lines)


async def compact_events(
    events: list[dict[str, Any]],
    *,
    model_cfg: ModelConfig,
    context_window_tokens: int,
    summarize_prompt: str = DEFAULT_SUMMARIZE_PROMPT,
) -> dict[str, Any] | None:
    model_events = _model_context_events(events)
    if len(model_events) < 4:
        return None

    conversation = events_to_conversation_text(model_events)
    if not conversation.strip():
        return None

    from pi_ai.stream import complete_simple
    from pi_ai.types import Context, TextContent, UserMessage
    from oma_adapter.web_fetch.summarize import resolve_pi_model

    model = resolve_pi_model(model_cfg)
    message = await complete_simple(
        model,
        Context(
            messages=[
                UserMessage(content=conversation),
                UserMessage(content=summarize_prompt),
            ],
        ),
        api_key=model_cfg.api_key,
        request_headers=model_cfg.custom_headers,
        thinking_level="off",
    )
    summary_parts: list[str] = []
    for block in message.content:
        if isinstance(block, TextContent):
            summary_parts.append(block.text)
    summary_text = "".join(summary_parts).strip()
    if not summary_text:
        return None

    return {
        "type": "agent.thread_context_compacted",
        "original_message_count": len(model_events),
        "compacted_message_count": 1,
        "summary": [{"type": "text", "text": summary_text}],
        "trigger": "auto",
        "pre_tokens": estimate_events_tokens(model_events),
        "context_window_tokens": context_window_tokens,
        "processed_at": time.time(),
    }
