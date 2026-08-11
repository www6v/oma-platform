"""OMA host bridge: wires pi_memory runtime to harness turn execution.

The memory system itself lives in the sibling ``piPy-hermes-memory`` repo
(``pi_memory`` package + ``extensions/memory_extension.py``). This bridge
only builds the per-turn :class:`pi_memory.runtime.MemoryRuntime` from the
OMA turn request and gates it on ``OMA_MEMORY_ENABLED=1``.
"""

from __future__ import annotations

import logging
import os
import uuid
from typing import Any

logger = logging.getLogger("oma.memory")

_MEMORY_ENABLED_ENV = "OMA_MEMORY_ENABLED"


def memory_enabled() -> bool:
    return os.environ.get(_MEMORY_ENABLED_ENV, "") == "1"


def _extract_last_user_text(events: list[dict[str, Any]] | None) -> str:
    """Last user message text from the session event history."""
    for event in reversed(events or []):
        if not isinstance(event, dict):
            continue
        if event.get("type") != "user.message":
            continue
        content = event.get("content")
        if isinstance(content, str):
            return content
        parts: list[str] = []
        if isinstance(content, list):
            for item in content:
                if isinstance(item, dict) and item.get("type") == "text":
                    text = item.get("text")
                    if text:
                        parts.append(str(text))
        if parts:
            return "\n".join(parts)
    return ""


def build_memory_runtime(
    *,
    session_id: str,
    tenant_id: str | None,
    agent_id: str,
    workdir: str,
    platform_base: str | None,
    internal_secret: str | None,
    events: list[dict[str, Any]] | None,
):
    """Build the per-turn MemoryRuntime, or None when disabled/incomplete."""
    if not memory_enabled():
        return None
    if not agent_id or not workdir:
        return None
    resolved_secret = internal_secret or os.environ.get("OMA_INTERNAL_SECRET")
    if not platform_base or not resolved_secret:
        logger.warning(
            "[memory] runtime skipped: platform_base/internal_secret missing"
        )
        return None
    try:
        from pi_memory.runtime import MemoryRuntime
    except ImportError:
        logger.warning("[memory] runtime skipped: pi_memory not installed")
        return None
    return MemoryRuntime(
        session_id=session_id,
        tenant_id=tenant_id or "default",
        agent_id=agent_id,
        workdir=workdir,
        platform_base=platform_base,
        internal_secret=resolved_secret,
        last_user_text=_extract_last_user_text(events),
        turn_uuid=uuid.uuid4().hex[:8],
    )


async def drain_memory_background_tasks(runtime) -> None:
    """Await provider sync tasks spawned during the turn (best effort)."""
    if runtime is None or not getattr(runtime, "background_tasks", None):
        return
    import asyncio

    await asyncio.gather(*runtime.background_tasks, return_exceptions=True)


def configure_runtime(runtime):
    """Set the pi_memory ContextVar; returns a reset token (or None)."""
    if runtime is None:
        return None
    try:
        from pi_memory.runtime import configure_memory_runtime
    except ImportError:
        return None
    return configure_memory_runtime(runtime)


def reset_runtime(token) -> None:
    if token is None:
        return
    try:
        from pi_memory.runtime import reset_memory_runtime
    except ImportError:
        return
    reset_memory_runtime(token)
