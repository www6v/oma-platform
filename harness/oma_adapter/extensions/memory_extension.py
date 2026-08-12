"""piPy extension: Hermes-style agent memory for OMA.

Loaded by the OMA harness (oma_adapter) when ``OMA_MEMORY_ENABLED=1``.
The host configures a :class:`pi_memory.runtime.MemoryRuntime` before the
piPy session is created; this extension then:

1. loads the agent's built-in memory (MEMORY.md / USER.md) from the
   platform internal API;
2. renders the frozen snapshot + provider context/recall into
   ``{workdir}/.pi/APPEND_SYSTEM.md`` (piPy appends it to the system
   prompt; register() runs before prompt resolution);
3. registers the ``memory`` tool plus provider-specific tools;
4. binds lifecycle hooks: turn_end → provider.sync_turn,
   session_before_compact → provider.on_pre_compress,
   agent_end → provider.on_session_end.
"""

from __future__ import annotations

import asyncio
import logging
from typing import Any

from pi_memory.builtin import BuiltinMemory, limits_from_env
from pi_memory.inject import render_memory_section, upsert_memory_section
from pi_memory.platform_client import PlatformMemoryClient
from pi_memory.providers import get_provider
from pi_memory.runtime import get_memory_runtime
from pi_memory.tools import MemoryTool, ProviderTool

logger = logging.getLogger("pi_memory.extension")

PREFETCH_TIMEOUT_SECONDS = 3.0


def _message_text(message: Any) -> str:
    """Extract plain text from a pi message (dict or object shape)."""
    if message is None:
        return ""
    if isinstance(message, dict):
        content = message.get("content")
    else:
        content = getattr(message, "content", None)
    if isinstance(content, str):
        return content
    parts: list[str] = []
    if isinstance(content, list):
        for item in content:
            if isinstance(item, dict):
                if item.get("type") == "text" and item.get("text"):
                    parts.append(str(item["text"]))
            elif getattr(item, "type", "") == "text":
                text = getattr(item, "text", "")
                if text:
                    parts.append(str(text))
    return "\n".join(parts)


async def register(api: Any) -> None:
    runtime = get_memory_runtime()
    if runtime is None:
        return

    client = PlatformMemoryClient(
        runtime.platform_base, runtime.internal_secret
    )
    contents: dict[str, str] = {}
    try:
        data = await client.get_agent_memory(
            runtime.tenant_id, runtime.agent_id
        )
        contents = data.get("contents") or {}
    except Exception as exc:
        logger.warning("built-in memory load failed: %s", exc)

    builtin = BuiltinMemory.from_store_contents(contents, limits_from_env())

    provider = get_provider()
    try:
        await provider.initialize(runtime)
    except Exception as exc:
        logger.warning("memory provider init failed: %s", exc)

    # ---- system prompt injection (frozen snapshot + provider context) ----
    blocks = builtin.render_blocks()
    try:
        static_block = provider.system_prompt_block()
    except Exception as exc:
        logger.warning("provider system_prompt_block failed: %s", exc)
        static_block = ""
    if static_block:
        blocks.append(static_block)
    if runtime.last_user_text.strip():
        try:
            recall = await asyncio.wait_for(
                provider.prefetch(runtime.last_user_text),
                timeout=PREFETCH_TIMEOUT_SECONDS,
            )
        except Exception as exc:
            logger.warning("provider prefetch failed: %s", exc)
            recall = ""
        if recall:
            blocks.append(recall)
    if blocks:
        try:
            upsert_memory_section(
                runtime.workdir, render_memory_section(blocks)
            )
        except OSError as exc:
            logger.warning("memory prompt injection failed: %s", exc)

    # ---- tools ----
    api.register_tool(MemoryTool(builtin, client, runtime, provider))
    try:
        for schema in provider.get_tool_schemas():
            if isinstance(schema, dict) and schema.get("name"):
                api.register_tool(ProviderTool(schema, provider))
    except Exception as exc:
        logger.warning("provider tool registration failed: %s", exc)

    # ---- lifecycle hooks ----
    async def _on_turn_end(event: dict[str, Any], ctx: Any) -> None:
        assistant_text = _message_text((event or {}).get("message"))
        if not (assistant_text or runtime.last_user_text):
            return
        task = asyncio.create_task(
            provider.sync_turn(runtime.last_user_text, assistant_text)
        )
        runtime.background_tasks.append(task)

    async def _on_before_compact(event: dict[str, Any], ctx: Any) -> None:
        runtime.background_tasks.append(
            asyncio.create_task(provider.on_pre_compress([]))
        )

    async def _on_agent_end(event: dict[str, Any], ctx: Any) -> None:
        messages = (event or {}).get("messages") or []
        runtime.background_tasks.append(
            asyncio.create_task(provider.on_session_end(messages))
        )

    try:
        api.on("turn_end", _on_turn_end)
        api.on("session_before_compact", _on_before_compact)
        api.on("agent_end", _on_agent_end)
    except Exception as exc:
        logger.warning("memory lifecycle hooks unavailable: %s", exc)
