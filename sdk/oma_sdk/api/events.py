"""
Events Examples - High-level helper functions for OMA-native session events.

Mirrors the pattern used by the other ``*Examples`` classes: each static
method performs a small end-to-end workflow (send a ``user.message``, list
events back, optionally stream them) and asserts on the response shape.

These helpers talk to the raw ``/v1/sessions/{id}/events*`` endpoints via
the OMA SDK's ``SessionEventsResource`` — *not* the Anthropic SDK's
``client.beta.sessions.events`` wrapper. Use them when you want direct
control over the event log (replay, cursor-based pagination, SSE tailing).
"""

from __future__ import annotations

import asyncio
import os
from typing import Any

_KEEP = os.getenv("OMA_KEEP_RESOURCES", "0") == "1"


class EventsExamples:
    """Example operations for OMA-native session events."""

    # -- internal -----------------------------------------------------------

    @staticmethod
    def _events(client: Any):
        """Resolve the ``SessionEventsResource`` from whatever client shape
        is passed. Accepts:

        * an ``OMAClient`` (has ``.events`` attribute)
        * a raw object with an ``events`` attribute (e.g. a test adapter)
        """
        events = getattr(client, "events", None)
        if events is None:
            raise TypeError(
                "EventsExamples requires a client with an 'events' attribute "
                "(e.g. OMAClient). Got: %r" % type(client).__name__
            )
        return events

    # -- send + list --------------------------------------------------------

    @staticmethod
    async def send_and_list(
        client: Any,
        session_id: str,
        *,
        message: str = "hello from events examples",
    ) -> dict:
        """Send a single ``user.message`` event and list the session's events.

        Returns::

            {"send_result": {...}, "events": [...]}
        """
        events = EventsExamples._events(client)

        send_result = await events.send(
            session_id,
            events=[{"type": "user.message", "content": message}],
        )
        assert send_result.get("status") == "queued"

        listed = await events.list(session_id)
        assert isinstance(listed.get("data"), list)
        assert "has_more" in listed

        return {"send_result": send_result, "events": listed["data"]}

    # -- list with pagination / order --------------------------------------

    @staticmethod
    async def list_paginated(
        client: Any,
        session_id: str,
        *,
        page_size: int = 2,
    ) -> dict:
        """Walk the event log one page at a time using ``next_page``.

        Returns::

            {"pages": [[event, ...], ...], "all": [event, ...]}
        """
        events = EventsExamples._events(client)
        pages: list[list[dict]] = []
        all_events: list[dict] = []

        next_page: str | None = None
        while True:
            page = await events.list(session_id, limit=page_size, next_page=next_page)
            pages.append(page["data"])
            all_events.extend(page["data"])
            if not page.get("has_more") or not page.get("next_page"):
                break
            next_page = page["next_page"]
            if len(pages) > 50:  # safety valve
                break

        return {"pages": pages, "all": all_events}

    @staticmethod
    async def list_descending(
        client: Any,
        session_id: str,
    ) -> dict:
        """List events in reverse-chronological order and confirm the shape."""
        events = EventsExamples._events(client)
        result = await events.list(session_id, order="desc", limit=10)
        assert isinstance(result.get("data"), list)
        return result

    # -- stream (SSE) -------------------------------------------------------

    @staticmethod
    async def stream_with_replay(
        client: Any,
        session_id: str,
        *,
        max_events: int = 64,
        timeout: float = 8.0,
    ) -> dict:
        """Open the SSE stream with ``replay=1``, collect up to
        ``max_events`` or ``timeout`` seconds of traffic — whichever first.

        The server replays every existing event and then keeps the
        connection open, emitting ``: keepalive`` comments every 15 s. We
        close after the replayed prefix is consumed (or the timeout fires)
        so the helper returns promptly.

        Returns::

            {"events": [event, ...], "timed_out": bool}
        """
        events = EventsExamples._events(client)
        collected: list[dict] = []
        timed_out = False

        async def _collect() -> None:
            async for ev in events.stream(session_id, replay=True):
                collected.append(ev)
                if len(collected) >= max_events:
                    return

        try:
            await asyncio.wait_for(_collect(), timeout=timeout)
        except asyncio.TimeoutError:
            timed_out = True

        return {"events": collected, "timed_out": timed_out}

    # -- sync helper --------------------------------------------------------

    @staticmethod
    def run_sync(coro):
        """Run ``coro`` from sync code. Creates a fresh event loop per call."""
        return asyncio.run(coro)
