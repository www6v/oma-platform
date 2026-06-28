"""
OMA-native session events resource.

Wraps the server's three events endpoints:

    POST /v1/sessions/{id}/events         -> send()
    GET  /v1/sessions/{id}/events         -> list()
    GET  /v1/sessions/{id}/events/stream  -> stream()  (SSE)

These are the raw server endpoints — distinct from the Anthropic SDK's
``client.beta.sessions.events`` wrapper, which speaks the managed-agents
protocol. Use this resource when you want direct access to the event log
(e.g. for replay, tailing, or feeding events from a non-Anthropic client).
"""

from __future__ import annotations

import json
from typing import Any, AsyncIterator

import httpx


class SessionEventsResource:
    """Send, list, and stream events on a session."""

    def __init__(self, http: httpx.AsyncClient) -> None:
        self._http = http

    # -- write --------------------------------------------------------------

    async def send(
        self,
        session_id: str,
        events: list[dict[str, Any]],
    ) -> dict:
        """Append one or more events to a session.

        The server validates each event's ``type`` against the allowed
        client-event set and either queues a turn (for ``user.message`` and
        other turn-triggering types) or simply persists the event.

        Returns ``{"status": "queued"}`` on success (HTTP 202).
        """
        r = await self._http.post(
            f"/v1/sessions/{session_id}/events",
            json={"events": events},
        )
        r.raise_for_status()
        return r.json()

    # -- read ---------------------------------------------------------------

    async def list(
        self,
        session_id: str,
        *,
        limit: int | None = None,
        order: str | None = None,
        after_seq: int | None = None,
        next_page: str | None = None,
    ) -> dict:
        """List events on a session.

        Parameters mirror the server's query string:

        * ``limit`` — page size (server default 100).
        * ``order`` — ``"asc"`` (default) or ``"desc"``.
        * ``after_seq`` / ``next_page`` — cursor pagination. The server
          accepts either; ``next_page`` takes the form ``"seq_<n>"``.

        Returns::

            {
                "data": [{"seq": int, "type": str, "ts": str, "data": {...}}, ...],
                "has_more": bool,
                "next_page": "seq_<n>" | None,
            }
        """
        params: dict[str, Any] = {}
        if limit is not None:
            params["limit"] = limit
        if order is not None:
            params["order"] = order
        if after_seq is not None:
            params["after_seq"] = after_seq
        if next_page is not None:
            params["next_page"] = next_page
        r = await self._http.get(
            f"/v1/sessions/{session_id}/events",
            params=params,
        )
        r.raise_for_status()
        return r.json()

    # -- SSE stream ---------------------------------------------------------

    async def stream(
        self,
        session_id: str,
        *,
        replay: bool = False,
        timeout: float | None = None,
    ) -> AsyncIterator[dict[str, Any]]:
        """Tail a session's event log as a server-sent event stream.

        Yields each event as a dict with the parsed ``data`` payload. The
        server's ``id:`` line (the event ``seq``) is merged into the yielded
        dict under the key ``seq`` so callers can do cursor-based pagination
        over the replayed prefix.

        Args:
            session_id: Session whose stream to subscribe to.
            replay: If ``True``, the server first emits every existing event
                (``replay=1``) before switching to live tailing.
            timeout: Optional override for the httpx stream timeout. The
                server sends a ``: keepalive`` comment every 15 s, so the
                default (30 s) gives ~1 missed keepalive before giving up.

        Example::

            async for ev in client.events.stream(session_id, replay=True):
                print(ev["seq"], ev.get("type"))
        """
        params: dict[str, Any] = {}
        if replay:
            params["replay"] = "1"

        # The server sends a ``: keepalive`` comment every 15 s. Default to
        # a 30 s read timeout so we tolerate one missed keepalive before
        # giving up; callers can override via ``timeout``.
        stream_kwargs: dict[str, Any] = {"params": params}
        if timeout is not None:
            stream_kwargs["timeout"] = timeout

        async with self._http.stream(
            "GET",
            f"/v1/sessions/{session_id}/events/stream",
            **stream_kwargs,
        ) as resp:
            resp.raise_for_status()
            async for event in _iter_sse_events(resp):
                yield event


# ---------------------------------------------------------------------------
# SSE parsing
# ---------------------------------------------------------------------------

async def _iter_sse_events(resp: httpx.Response) -> AsyncIterator[dict[str, Any]]:
    """Parse an SSE response into event dicts.

    The wire format (per the server's ``writeSSE``)::

        id: <seq>\\n
        data: <json>\\n
        \\n

    Comment lines (``: ...``) are keepalives and are silently dropped.
    """
    current_id: str | None = None
    current_data: str | None = None

    # httpx's aiter_lines handles both \\n and \\r\\n; it also strips the
    # trailing newline from each line.
    async for raw in resp.aiter_lines():
        line = raw.rstrip("\r")
        if not line:
            # Blank line terminates the event block.
            if current_data is not None:
                event = _parse_sse_event(current_id, current_data)
                if event is not None:
                    yield event
            current_id = None
            current_data = None
            continue

        if line.startswith(":"):
            # Comment / keepalive — ignore.
            continue

        if line.startswith("id:"):
            current_id = line[3:].strip()
        elif line.startswith("data:"):
            # Per SSE spec, data can span multiple ``data:`` lines joined
            # by newlines. The server emits a single line per event, but
            # we accumulate defensively.
            chunk = line[5:]
            if chunk.startswith(" "):
                chunk = chunk[1:]
            current_data = chunk if current_data is None else f"{current_data}\n{chunk}"
        # Other field names (event:, retry:) are ignored — the server
        # doesn't emit them.


def _parse_sse_event(
    event_id: str | None,
    data: str,
) -> dict[str, Any] | None:
    """Parse one SSE event block into a dict.

    ``data`` is the raw JSON body; ``event_id`` (the ``seq``) is folded in
    as ``seq`` so callers can use it as a cursor.
    """
    try:
        payload = json.loads(data)
    except json.JSONDecodeError:
        # Non-JSON data line — surface as a raw-text event so the caller
        # can still see it, rather than silently dropping.
        payload = {"_raw": data}

    if not isinstance(payload, dict):
        payload = {"_raw": payload}

    if event_id is not None:
        try:
            payload.setdefault("seq", int(event_id))
        except ValueError:
            payload.setdefault("seq", event_id)
    return payload
