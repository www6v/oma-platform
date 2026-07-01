"""
Cookbook parity helpers for managed-agents examples.

Mirrors Anthropic ``managed_agents/utilities.py`` patterns using OMA's async
httpx SSE resource (``client.events.stream``) instead of the sync anthropic
``with client.beta.sessions.events.stream(...)`` context manager.
"""

from __future__ import annotations

import asyncio
import time
from dataclasses import dataclass
from typing import Any, Callable

# Default timeouts match cookbook example scripts.
DEFAULT_TIMEOUT_SEC = 900.0
DEFAULT_STREAM_READ_TIMEOUT = 300.0
DEFAULT_STREAM_CONNECT_DELAY = 0.1
DEFAULT_IDLE_POLL_MAX_WAIT = 30.0


@dataclass(frozen=True)
class StreamConfig:
    """Timeouts for ``stream_until_end_turn`` and ``wait_for_idle_status``."""

    timeout_sec: float = DEFAULT_TIMEOUT_SEC
    stream_read_timeout: float = DEFAULT_STREAM_READ_TIMEOUT
    stream_connect_delay: float = DEFAULT_STREAM_CONNECT_DELAY
    idle_poll_max_wait: float = DEFAULT_IDLE_POLL_MAX_WAIT


def event_type(ev: dict[str, Any]) -> str | None:
    """Return the logical event type from an SSE or list payload."""
    if ev.get("type"):
        return str(ev["type"])
    data = ev.get("data")
    if isinstance(data, dict) and data.get("type"):
        return str(data["type"])
    return None


def event_payload(ev: dict[str, Any]) -> dict[str, Any]:
    """Unwrap nested ``data`` when the server nests the event body."""
    data = ev.get("data")
    if isinstance(data, dict) and data.get("type"):
        return data
    return ev


def stop_reason_type(payload: dict[str, Any]) -> str | None:
    """Extract ``stop_reason.type`` from a ``session.status_idle`` payload."""
    stop_reason = payload.get("stop_reason")
    if isinstance(stop_reason, dict):
        reason_type = stop_reason.get("type")
        if reason_type:
            return str(reason_type)
    return None


def message_text(payload: dict[str, Any]) -> str:
    """Extract concatenated text blocks from a message event payload."""
    parts: list[str] = []
    content = payload.get("content")
    if isinstance(content, str):
        return content.strip()
    for block in content or []:
        if not isinstance(block, dict):
            continue
        if block.get("type") == "text" and block.get("text"):
            parts.append(str(block["text"]))
    return "\n".join(parts).strip()


def print_stream_event(
    ev: dict[str, Any],
    *,
    preview_length: int | None = 300,
) -> None:
    """Print agent.message / tool_use progress from a stream event.

    Args:
        preview_length: Truncate agent.message text to this many chars.
            ``None`` prints the full message (iterate notebook style).
    """
    ev_type = event_type(ev)
    if not ev_type:
        return
    payload = event_payload(ev)
    if ev_type == "agent.message":
        text = message_text(payload)
        if not text:
            return
        if preview_length is None:
            print(text, end="")
        else:
            preview = text[:preview_length] + (
                "..." if len(text) > preview_length else ""
            )
            print(preview)
    elif ev_type in ("agent.tool_use", "agent.mcp_tool_use"):
        name = payload.get("name") or ""
        prefix = "\n" if preview_length is None else "  "
        print(f"{prefix}[{name}]")
    elif ev_type == "session.error":
        msg = payload.get("message") or payload.get("error") or "session.error"
        raise RuntimeError(f"Session error: {msg}")
    elif ev_type == "session.status_terminated":
        raise RuntimeError("Session terminated before end_turn")


async def wait_for_idle_status(
    client: Any,
    session_id: str,
    *,
    max_wait: float = DEFAULT_IDLE_POLL_MAX_WAIT,
) -> None:
    """Poll until ``sessions.retrieve().status == "idle"``.

    Cookbook ``utilities.wait_for_idle_status``: SSE ``session.status_idle``
    can arrive before the session record shows idle; calling ``archive()``
    immediately after the stream exits may 400.
    """
    deadline = time.monotonic() + max_wait
    while time.monotonic() < deadline:
        resp = await client._http.get(f"/v1/sessions/{session_id}")
        resp.raise_for_status()
        if resp.json().get("status") == "idle":
            return
        await asyncio.sleep(0.25)
    raise TimeoutError(
        f"session {session_id} status did not settle to idle within {max_wait}s"
    )


async def stream_until_end_turn(
    client: Any,
    session_id: str,
    *,
    send_events: list[dict[str, Any]] | None = None,
    config: StreamConfig | None = None,
    on_event: Callable[[dict[str, Any]], None] | None = None,
) -> None:
    """Stream session events until ``session.status_idle`` + ``end_turn``.

    Cookbook canonical order (``CMA_iterate_fix_failing_tests`` Cell 11):

        with client.beta.sessions.events.stream(session.id) as stream:
            client.beta.sessions.events.send(...)
            for ev in stream: ...

    OMA uses async httpx SSE. When ``send_events`` is provided, this helper
    opens the stream first, waits ``stream_connect_delay``, then sends — with
    ``replay=True`` so events are not lost if ordering races.

    Exit condition: ``session.status_idle`` **and**
    ``stop_reason.type == "end_turn"``. Bare idle also fires for custom-tool
    ``requires_action`` turns; do not treat that as turn completion.
    """
    cfg = config or StreamConfig()
    deadline = time.time() + cfg.timeout_sec
    end_turn_seen = asyncio.Event()
    stream_error: Exception | None = None
    handler = on_event or (
        lambda ev: print_stream_event(ev, preview_length=300)
    )

    # Ignore replayed session.status_idle/end_turn from prior turns (MT1).
    start_seq = 0
    try:
        listed = await client.events.list(session_id, order="desc", limit=1)
        rows = listed.get("data") or []
        if rows:
            start_seq = int(rows[0].get("seq") or 0)
    except Exception:
        pass

    async def consume() -> None:
        nonlocal stream_error
        try:
            async for ev in client.events.stream(
                session_id,
                replay=True,
                timeout=cfg.stream_read_timeout,
            ):
                handler(ev)
                ev_type = event_type(ev)
                if ev_type == "session.status_idle":
                    payload = event_payload(ev)
                    ev_seq = int(ev.get("seq") or 0)
                    if (
                        stop_reason_type(payload) == "end_turn"
                        and ev_seq > start_seq
                    ):
                        end_turn_seen.set()
                        return
                if time.time() >= deadline:
                    break
        except Exception as exc:
            stream_error = exc
            end_turn_seen.set()

    stream_task = asyncio.create_task(consume())

    await asyncio.sleep(cfg.stream_connect_delay)
    if send_events is not None:
        client.sessions.events.send(session_id, events=send_events)

    remaining = max(1.0, deadline - time.time())
    try:
        await asyncio.wait_for(end_turn_seen.wait(), timeout=remaining)
    except asyncio.TimeoutError as exc:
        stream_task.cancel()
        raise TimeoutError(
            f"Timed out after {cfg.timeout_sec}s waiting for end_turn"
        ) from exc
    finally:
        if not stream_task.done():
            stream_task.cancel()
            try:
                await stream_task
            except asyncio.CancelledError:
                pass

    if stream_error is not None:
        raise stream_error

    await wait_for_idle_status(
        client,
        session_id,
        max_wait=cfg.idle_poll_max_wait,
    )
