"""E2E tests for the OMA-native events API.

These tests exercise the raw ``/v1/sessions/{id}/events*`` endpoints via
``OMAClient.events`` (an async httpx-backed resource). A real session is
created through the sync ``anthropic.Anthropic`` fixture (matching the
rest of the suite), then the async events API is driven against it.
"""

from __future__ import annotations

import asyncio
import os

import anthropic
import httpx
import pytest
import pytest_asyncio

from oma_sdk import OMAClient
from oma_sdk.examples import EventsExamples, SessionExamples


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

@pytest_asyncio.fixture
async def oma_client():
    """Return an ``OMAClient`` wired at the test server; close after the test."""
    client = OMAClient(base_url=os.getenv("OMA_BASE_URL", "http://localhost:8787"))
    yield client
    await client.aclose()


@pytest.fixture
def live_session(client: anthropic.Anthropic, tmp_agent, tmp_env):
    """Create a real session via the Anthropic SDK and archive it afterwards.

    Yields the session id — tests drive the async events API against it.
    We create the session here (sync) rather than inside each test so we
    don't blow the 5-creates/60s rate limit.
    """
    sess = SessionExamples._create_session(client, tmp_agent.id, tmp_env)
    try:
        yield sess.id
    finally:
        if os.getenv("OMA_KEEP_RESOURCES", "0") != "1":
            try:
                client.beta.sessions.archive(sess.id)
            except Exception:
                pass


# ---------------------------------------------------------------------------
# send / list
# ---------------------------------------------------------------------------

@pytest.mark.asyncio
async def test_send_and_list_events(oma_client: OMAClient, live_session: str):
    """Send a single user.message event and list it back."""
    send_result = await oma_client.events.send(
        live_session,
        events=[{"type": "user.message", "content": "hello from events e2e"}],
    )
    assert send_result.get("status") == "queued"

    listed = await oma_client.events.list(live_session)
    assert isinstance(listed.get("data"), list)
    assert "has_more" in listed

    types = [e.get("type") for e in listed["data"]]
    assert "user.message" in types


@pytest.mark.asyncio
async def test_send_multiple_events_in_one_call(
    oma_client: OMAClient, live_session: str
):
    """The server accepts a batch of events per POST."""
    send_result = await oma_client.events.send(
        live_session,
        events=[
            {"type": "user.message", "content": "one"},
            {"type": "user.message", "content": "two"},
        ],
    )
    assert send_result["status"] == "queued"

    listed = await oma_client.events.list(live_session)
    user_msgs = [e for e in listed["data"] if e.get("type") == "user.message"]
    assert len(user_msgs) >= 2


@pytest.mark.asyncio
async def test_list_events_shape_has_seq_type_ts_data(
    oma_client: OMAClient, live_session: str
):
    """Each listed event carries ``seq``, ``type``, ``ts``, ``data``."""
    await oma_client.events.send(
        live_session,
        events=[{"type": "user.message", "content": "shape-check"}],
    )
    listed = await oma_client.events.list(live_session)
    assert listed["data"], "expected at least one event"

    sample = listed["data"][-1]
    assert "seq" in sample and isinstance(sample["seq"], int)
    assert "type" in sample and sample["type"] == "user.message"
    assert "ts" in sample and isinstance(sample["ts"], str)
    assert "data" in sample


# ---------------------------------------------------------------------------
# Pagination / order
# ---------------------------------------------------------------------------

@pytest.mark.asyncio
async def test_list_supports_limit_and_order(
    oma_client: OMAClient, live_session: str
):
    """Exercise ``limit`` and ``order`` kwargs."""
    # Send all events in a single batch to avoid racing with the agent's
    # turn processing (``user.message`` triggers a turn; rapid back-to-back
    # posts can 409 with ``session busy`` while the previous turn is queued).
    await oma_client.events.send(
        live_session,
        events=[
            {"type": "user.message", "content": "msg-0"},
            {"type": "user.message", "content": "msg-1"},
            {"type": "user.message", "content": "msg-2"},
        ],
    )

    asc = await oma_client.events.list(live_session, limit=2, order="asc")
    assert isinstance(asc["data"], list)
    assert len(asc["data"]) <= 2
    assert "has_more" in asc

    desc = await oma_client.events.list(live_session, limit=2, order="desc")
    assert isinstance(desc["data"], list)
    assert len(desc["data"]) <= 2


@pytest.mark.asyncio
async def test_list_pagination_with_next_page(
    oma_client: OMAClient, live_session: str
):
    """Walk through events one page at a time using ``next_page``."""
    # Seed enough events for multiple pages in a single batched POST.
    # Batching avoids 409s from racing with the agent's turn processing
    # (each ``user.message`` triggers a turn; sequential POSTs race).
    await oma_client.events.send(
        live_session,
        events=[
            {"type": "user.message", "content": f"page-{i}"}
            for i in range(3)
        ],
    )

    all_seqs: list[int] = []
    next_page: str | None = None
    pages = 0

    while True:
        page = await oma_client.events.list(
            live_session, limit=1, next_page=next_page
        )
        pages += 1
        for ev in page["data"]:
            all_seqs.append(ev["seq"])

        if not page.get("has_more") or not page.get("next_page"):
            break
        next_page = page["next_page"]
        if pages > 20:  # safety valve
            break

    assert pages >= 2, "expected at least two pages with limit=1 and 3 events"
    # seqs should be strictly ascending (default order)
    assert all_seqs == sorted(all_seqs)
    assert len(set(all_seqs)) == len(all_seqs), "seqs should be unique"


# ---------------------------------------------------------------------------
# Stream (SSE)
# ---------------------------------------------------------------------------

@pytest.mark.asyncio
async def test_stream_with_replay_returns_existing_events(
    oma_client: OMAClient, live_session: str
):
    """``replay=True`` must surface events that were sent before the stream."""
    await oma_client.events.send(
        live_session,
        events=[{"type": "user.message", "content": "pre-stream"}],
    )

    collected: list[dict] = []

    async def _collect() -> None:
        async for ev in oma_client.events.stream(live_session, replay=True):
            collected.append(ev)
            # Stop once we've seen the pre-stream event.
            if any(e.get("type") == "user.message" for e in collected):
                return

    await asyncio.wait_for(_collect(), timeout=8.0)
    assert collected, "expected at least one replayed event"
    types = [e.get("type") for e in collected]
    assert "user.message" in types


@pytest.mark.asyncio
async def test_stream_events_carry_seq(
    oma_client: OMAClient, live_session: str
):
    """Every replayed event must have a numeric ``seq`` (from the SSE ``id:``)."""
    await oma_client.events.send(
        live_session,
        events=[{"type": "user.message", "content": "seq-check"}],
    )

    collected: list[dict] = []

    async def _collect() -> None:
        async for ev in oma_client.events.stream(live_session, replay=True):
            collected.append(ev)
            if len(collected) >= 4:
                return

    await asyncio.wait_for(_collect(), timeout=8.0)
    assert collected
    for ev in collected:
        assert "seq" in ev
        assert isinstance(ev["seq"], int)


@pytest.mark.asyncio
async def test_stream_timeout_exits_cleanly(
    oma_client: OMAClient, live_session: str
):
    """Without ``replay`` and with no traffic, the stream should time out
    cleanly (not hang forever)."""
    collected: list[dict] = []

    async def _collect() -> None:
        async for ev in oma_client.events.stream(live_session, replay=False):
            collected.append(ev)

    with pytest.raises(asyncio.TimeoutError):
        await asyncio.wait_for(_collect(), timeout=1.0)


# ---------------------------------------------------------------------------
# EventsExamples helpers
# ---------------------------------------------------------------------------

@pytest.mark.asyncio
async def test_events_examples_send_and_list(
    oma_client: OMAClient, live_session: str
):
    """Exercise the high-level ``EventsExamples.send_and_list`` helper."""
    result = await EventsExamples.send_and_list(oma_client, live_session)
    assert result["send_result"]["status"] == "queued"
    assert isinstance(result["events"], list)


@pytest.mark.asyncio
async def test_events_examples_list_descending(
    oma_client: OMAClient, live_session: str
):
    """Exercise ``EventsExamples.list_descending``."""
    await oma_client.events.send(
        live_session,
        events=[{"type": "user.message", "content": "desc-helper"}],
    )
    result = await EventsExamples.list_descending(oma_client, live_session)
    assert isinstance(result.get("data"), list)


@pytest.mark.asyncio
async def test_events_examples_stream_with_replay(
    oma_client: OMAClient, live_session: str
):
    """Exercise ``EventsExamples.stream_with_replay`` — should return
    promptly (with a timeout) rather than hang."""
    await oma_client.events.send(
        live_session,
        events=[{"type": "user.message", "content": "stream-helper"}],
    )
    result = await EventsExamples.stream_with_replay(
        oma_client, live_session, timeout=4.0
    )
    assert isinstance(result.get("events"), list)
    assert result["events"], "expected at least one replayed event"


# ---------------------------------------------------------------------------
# Error paths
# ---------------------------------------------------------------------------

@pytest.mark.asyncio
async def test_send_empty_events_array_returns_400(
    oma_client: OMAClient, live_session: str
):
    """Server rejects an empty events array (400)."""
    with pytest.raises(httpx.HTTPStatusError) as excinfo:
        await oma_client.events.send(live_session, events=[])
    assert excinfo.value.response.status_code == 400


@pytest.mark.asyncio
async def test_send_invalid_event_type_returns_400(
    oma_client: OMAClient, live_session: str
):
    """Server rejects events with an unknown ``type`` (400)."""
    with pytest.raises(httpx.HTTPStatusError) as excinfo:
        await oma_client.events.send(
            live_session,
            events=[{"type": "not.a.real.event.type"}],
        )
    assert excinfo.value.response.status_code == 400


@pytest.mark.asyncio
async def test_list_events_unknown_session_returns_500_or_404(
    oma_client: OMAClient,
):
    """Listing events for a non-existent session is an error."""
    with pytest.raises(httpx.HTTPStatusError):
        await oma_client.events.list("sess_does_not_exist_xyz")


@pytest.mark.asyncio
async def test_events_examples_rejects_client_without_events_attr():
    """``EventsExamples`` requires a client with an ``events`` attribute."""
    with pytest.raises(TypeError, match="events"):
        await EventsExamples.send_and_list(object(), "sess_abc")
