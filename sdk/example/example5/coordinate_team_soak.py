"""CT6 live soak runner — coordinate specialist team cookbook."""

from __future__ import annotations

import os
from typing import Any

from oma_sdk import OMAClient, StreamConfig, stream_until_end_turn, wait_for_idle_status
from oma_sdk.cookbook import event_payload, event_type
from oma_sdk.subagent import (
    SPECIALIST_LIBRARIAN,
    SPECIALIST_PRICER,
    SPECIALIST_RESEARCHER,
    build_multiagent,
    count_thread_created,
    count_thread_message_received,
)

from coordinate_fixtures import (
    CASE_STUDY_FILES,
    COORDINATOR_NAME,
    COORDINATOR_SYSTEM,
    COORDINATOR_TOOLS,
    ENV_NAME,
    LIBRARIAN_SYSTEM,
    LIBRARIAN_TOOLS,
    MOUNT_PRICING,
    MOUNT_PRODUCT,
    PRICER_SYSTEM,
    PRICER_TOOLS,
    PROPOSAL_FILENAME,
    RESEARCHER_SYSTEM,
    RESEARCHER_TOOLS,
    SESSION_TITLE,
    build_prospect_message,
    case_study_mount_path,
    fixture_path,
)


def _stream_config() -> StreamConfig:
    return StreamConfig(
        timeout_sec=float(os.getenv("OMA_COORDINATE_TIMEOUT_SEC", "1800")),
        stream_read_timeout=float(
            os.getenv("OMA_STREAM_READ_TIMEOUT_SEC", "600"),
        ),
        idle_poll_max_wait=float(
            os.getenv("OMA_IDLE_POLL_MAX_WAIT_SEC", "120"),
        ),
    )


def normalize_events(rows: list[dict[str, Any]]) -> list[dict[str, Any]]:
    out: list[dict[str, Any]] = []
    for row in rows:
        payload = row.get("data") if isinstance(row.get("data"), dict) else row
        if isinstance(payload, dict):
            out.append(payload)
    return out


async def _list_session_events(
    client: OMAClient,
    session_id: str,
) -> list[dict[str, Any]]:
    listed = await client.events.list(session_id, order="asc", limit=5000)
    return listed.get("data") or []


async def upload_fixture(
    client: OMAClient,
    rel_path: str,
    mount_name: str,
) -> str:
    path = fixture_path(rel_path)
    uploaded = await client.files.upload(
        filename=mount_name,
        content=path.read_text(encoding="utf-8"),
        media_type="text/markdown",
        downloadable=True,
    )
    return uploaded["id"]


async def _session_has_proposal(client: OMAClient, session_id: str) -> bool:
    outputs = await client.files.list(scope_id=session_id)
    files = outputs.get("data") or []
    return any(f.get("filename") == PROPOSAL_FILENAME for f in files)


def _saw_web_search(events: list[dict[str, Any]]) -> bool:
    for ev in events:
        if event_type(ev) != "agent.tool_use":
            continue
        payload = event_payload(ev)
        name = payload.get("name") or payload.get("tool_name") or ""
        if name == "web_search":
            return True
    return False


def _count_call_agent_uses(events: list[dict[str, Any]]) -> int:
    count = 0
    for ev in events:
        if event_type(ev) != "agent.tool_use":
            continue
        payload = event_payload(ev)
        name = str(payload.get("name") or payload.get("tool_name") or "")
        if name.startswith("call_agent_"):
            count += 1
    return count


async def run_coordinate_team_soak(
    client: OMAClient,
    *,
    model: str,
    keep_resources: bool = False,
    networking: str = "unrestricted",
) -> dict[str, Any]:
    """Run the full coordinate specialist team live soak (CT6).

    Returns summary with thread counts, proposal output, and delegation signals.
    """
    product_id = await upload_fixture(
        client, "northstar/product_one_pager.md", "product_one_pager.md",
    )
    pricing_id = await upload_fixture(
        client, "northstar/pricing_rules.md", "pricing_rules.md",
    )
    case_ids: dict[str, str] = {}
    for filename in CASE_STUDY_FILES:
        case_ids[filename] = await upload_fixture(
            client,
            f"northstar/case_studies/{filename}",
            filename,
        )

    researcher = client.agents.create(
        name=SPECIALIST_RESEARCHER,
        model={"id": model},
        system=RESEARCHER_SYSTEM,
        tools=RESEARCHER_TOOLS,
    )
    librarian = client.agents.create(
        name=SPECIALIST_LIBRARIAN,
        model={"id": model},
        system=LIBRARIAN_SYSTEM,
        tools=LIBRARIAN_TOOLS,
    )
    pricer = client.agents.create(
        name=SPECIALIST_PRICER,
        model={"id": model},
        system=PRICER_SYSTEM,
        tools=PRICER_TOOLS,
    )

    coordinator = client.agents.create(
        name=COORDINATOR_NAME,
        model={"id": model},
        system=COORDINATOR_SYSTEM,
        tools=COORDINATOR_TOOLS,
        multiagent=build_multiagent(
            [researcher.id, librarian.id, pricer.id],
            versions={
                researcher.id: researcher.version,
                librarian.id: librarian.version,
                pricer.id: pricer.version,
            },
        ),
    )

    resources: list[dict[str, Any]] = [
        {
            "type": "file",
            "file_id": product_id,
            "mount_path": MOUNT_PRODUCT,
        },
        {
            "type": "file",
            "file_id": pricing_id,
            "mount_path": MOUNT_PRICING,
        },
    ]
    for filename, file_id in case_ids.items():
        resources.append(
            {
                "type": "file",
                "file_id": file_id,
                "mount_path": case_study_mount_path(filename),
            },
        )

    env = client.environments.create(
        name=ENV_NAME,
        config={"type": "cloud", "networking": {"type": networking}},
    )
    session = client.sessions.create(
        environment_id=env.id,
        agent={"type": "agent", "id": coordinator.id, "version": coordinator.version},
        title=SESSION_TITLE,
        resources=resources,
    )

    streamed_events: list[dict[str, Any]] = []

    def _on_event(ev: dict[str, Any]) -> None:
        streamed_events.append(ev)

    cfg = _stream_config()
    await stream_until_end_turn(
        client,
        session.id,
        send_events=[
            {
                "type": "user.message",
                "content": build_prospect_message(),
            },
        ],
        config=cfg,
        on_event=_on_event,
    )
    await wait_for_idle_status(
        client,
        session.id,
        max_wait=cfg.idle_poll_max_wait,
    )

    event_rows = await _list_session_events(client, session.id)
    events = normalize_events(event_rows)
    thread_created = count_thread_created(events)
    thread_received = count_thread_message_received(events)
    has_proposal = await _session_has_proposal(client, session.id)
    saw_web_search = _saw_web_search(events)
    call_agent_uses = _count_call_agent_uses(events)

    threads_resp = await client._http.get(
        f"/v1/sessions/{session.id}/threads",
    )
    threads_resp.raise_for_status()
    threads_body = threads_resp.json()
    threads = threads_body.get("data") or []

    if not keep_resources and os.getenv("OMA_KEEP_RESOURCES") != "1":
        client.sessions.archive(session.id)
        client.environments.archive(env.id)
        for agent_id in (
            coordinator.id,
            researcher.id,
            librarian.id,
            pricer.id,
        ):
            try:
                client.agents.archive(agent_id)
            except Exception:
                pass

    return {
        "session_id": session.id,
        "environment_id": env.id,
        "coordinator_id": coordinator.id,
        "researcher_id": researcher.id,
        "librarian_id": librarian.id,
        "pricer_id": pricer.id,
        "thread_created_count": thread_created,
        "thread_received_count": thread_received,
        "threads_count": len(threads),
        "call_agent_uses": call_agent_uses,
        "saw_web_search": saw_web_search,
        "has_proposal": has_proposal,
        "stream_event_count": len(streamed_events),
    }
