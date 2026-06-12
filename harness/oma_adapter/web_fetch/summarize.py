"""Aux-model summarization for large web_fetch results."""

from __future__ import annotations

import time
import uuid
from datetime import datetime, timezone
from typing import Any

from pi_ai.stream import complete_simple
from pi_ai.types import AssistantMessage, Context, TextContent, UserMessage
from oma_adapter.web_fetch.runtime import WebFetchRuntime

WEB_SUMMARIZE_SYSTEM_PROMPT = """Compress the page below into a digest for an autonomous agent that fetched this URL while researching something. Assume the agent will not re-read the original.

Keep:
- Every concrete fact (numbers, dates, named entities, identifiers, statuses, geographic regions, units)
- Tables and lists in their original layout — do not narrativize them
- Outbound links that look like data sources (don't paraphrase URLs)
- Quoted material relevant to the page's topic, verbatim

Drop:
- Site chrome (nav, ads, footers, cookie banners, legal disclaimers, "share this article")
- Restated headers, repeated boilerplate, meta-commentary about the page itself
- Marketing copy that adds no information

Format: short markdown. Headings only when the original had them. No introduction. No "this page describes…" — just the content.

If the page is an error/404/login wall/empty result, output exactly one line stating that and stop."""


def _assistant_text(message: AssistantMessage) -> str:
    parts: list[str] = []
    for block in message.content:
        if isinstance(block, TextContent):
            parts.append(block.text)
    return "".join(parts).strip()


def resolve_pi_model(cfg: ModelConfig) -> Any:
    from pi_ai.models import get_model

    provider = (cfg.provider or "").strip()
    model_id = cfg.model.strip()
    if not provider and "/" in model_id:
        provider, model_id = model_id.split("/", 1)
    if not provider:
        provider = "anthropic"
    return get_model(provider, model_id)


async def summarize_page_markdown(
    *,
    url: str,
    markdown: str,
    aux_cfg: ModelConfig,
    runtime: WebFetchRuntime,
) -> tuple[str, dict[str, int]]:
    """Call aux model; emit aux.model_call when configured."""
    model = resolve_pi_model(aux_cfg)
    model_id = aux_cfg.model
    t0 = time.monotonic()
    usage = {"input": 0, "output": 0}
    try:
        message = await complete_simple(
            model,
            Context(
                system_prompt=WEB_SUMMARIZE_SYSTEM_PROMPT,
                messages=[
                    UserMessage(
                        content=(
                            f"URL: {url}\n\n"
                            f"PAGE CONTENT (markdown):\n\n{markdown}"
                        ),
                    ),
                ],
            ),
            api_key=aux_cfg.api_key,
            request_headers=aux_cfg.custom_headers,
            thinking_level="off",
        )
        summary = _assistant_text(message)
        if not summary:
            raise RuntimeError("aux model returned empty summary")
        usage = {
            "input": message.usage.input,
            "output": message.usage.output,
        }
        await _emit_aux_event(
            runtime=runtime,
            model_id=model_id,
            task="web_summarize",
            duration_ms=int((time.monotonic() - t0) * 1000),
            tokens=usage,
            status="ok",
        )
        return summary, usage
    except Exception as exc:
        await _emit_aux_event(
            runtime=runtime,
            model_id=model_id,
            task="web_summarize",
            duration_ms=int((time.monotonic() - t0) * 1000),
            tokens=usage,
            status="failed",
            error=str(exc),
        )
        raise


async def _emit_aux_event(
    *,
    runtime: WebFetchRuntime,
    model_id: str,
    task: str,
    duration_ms: int,
    tokens: dict[str, int],
    status: str,
    error: str | None = None,
) -> None:
    if runtime.emit_event is None:
        return
    payload: dict[str, Any] = {
        "type": "aux.model_call",
        "id": f"sevt-{uuid.uuid4().hex[:12]}",
        "processed_at": datetime.now(timezone.utc).isoformat(),
        "model_id": model_id,
        "task": task,
        "duration_ms": duration_ms,
        "tokens": tokens,
        "status": status,
    }
    if error:
        payload["error"] = error
    await runtime.emit_event(payload)
