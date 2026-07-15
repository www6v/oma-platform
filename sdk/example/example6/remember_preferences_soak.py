"""RP live soak runner — remember user preferences cookbook."""

from __future__ import annotations

import os
from typing import Any

from oma_sdk import OMAClient, StreamConfig, stream_until_end_turn, wait_for_idle_status

from remember_fixtures import (
    AGENT_NAME,
    AGENT_SYSTEM,
    AGENT_TOOLS,
    ENV_NAME,
    MEMORY_STORE_DESCRIPTION,
    MEMORY_STORE_NAME,
    PREFERENCE_PATH,
    RECALL_USER_MESSAGE,
    SAVE_USER_MESSAGE,
    SESSION_RECALL_TITLE,
    SESSION_SAVE_TITLE,
    build_memory_resource,
)


def _stream_config() -> StreamConfig:
    return StreamConfig(
        timeout_sec=float(os.getenv("OMA_REMEMBER_TIMEOUT_SEC", "600")),
        stream_read_timeout=float(
            os.getenv("OMA_STREAM_READ_TIMEOUT_SEC", "300"),
        ),
        idle_poll_max_wait=float(
            os.getenv("OMA_IDLE_POLL_MAX_WAIT_SEC", "60"),
        ),
    )


async def _memory_at_path(
    client: OMAClient,
    store_id: str,
    path: str,
) -> str | None:
    listed = await client._http.get(
        f"/v1/memory_stores/{store_id}/memories",
    )
    listed.raise_for_status()
    for row in listed.json().get("data") or []:
        if row.get("path") != path:
            continue
        mem_id = row.get("id")
        if not mem_id:
            continue
        detail = await client._http.get(
            f"/v1/memory_stores/{store_id}/memories/{mem_id}",
        )
        detail.raise_for_status()
        content = detail.json().get("content")
        if isinstance(content, str) and content.strip():
            return content
    return None


async def run_remember_preferences_soak(
    client: OMAClient,
    *,
    model: str,
    keep_resources: bool = False,
) -> dict[str, Any]:
    """Run two-session remember preferences soak (RP1–RP3)."""
    store = client.memory_stores.create(
        name=MEMORY_STORE_NAME,
        description=MEMORY_STORE_DESCRIPTION,
    )
    agent = client.agents.create(
        name=AGENT_NAME,
        model={"id": model},
        system=AGENT_SYSTEM,
        tools=AGENT_TOOLS,
    )
    env = client.environments.create(
        name=ENV_NAME,
        config={
            "type": "sandbox",
            "sandbox": {
                "provider": "opensandbox",
                "opensandbox": {"image": "python:3.12-slim"},
            },
        },
    )
    resource = build_memory_resource(store.id)

    session1 = client.sessions.create(
        environment_id=env.id,
        agent={"type": "agent", "id": agent.id, "version": agent.version},
        title=SESSION_SAVE_TITLE,
        resources=[resource],
    )
    cfg = _stream_config()
    await stream_until_end_turn(
        client,
        session1.id,
        send_events=[
            {
                "type": "user.message",
                "content": [{"type": "text", "text": SAVE_USER_MESSAGE}],
            },
        ],
        config=cfg,
    )
    await wait_for_idle_status(client, session1.id, max_wait=cfg.idle_poll_max_wait)

    saved_content = await _memory_at_path(client, store.id, PREFERENCE_PATH)

    session2 = client.sessions.create(
        environment_id=env.id,
        agent={"type": "agent", "id": agent.id, "version": agent.version},
        title=SESSION_RECALL_TITLE,
        resources=[resource],
    )
    await stream_until_end_turn(
        client,
        session2.id,
        send_events=[
            {
                "type": "user.message",
                "content": [{"type": "text", "text": RECALL_USER_MESSAGE}],
            },
        ],
        config=cfg,
    )
    await wait_for_idle_status(client, session2.id, max_wait=cfg.idle_poll_max_wait)

    recalled = await _memory_at_path(client, store.id, PREFERENCE_PATH)

    if not keep_resources and os.getenv("OMA_KEEP_RESOURCES") != "1":
        client.sessions.archive(session1.id)
        client.sessions.archive(session2.id)
        client.agents.archive(agent.id)
        client.environments.archive(env.id)
        client.memory_stores.archive(store.id)

    return {
        "memory_store_id": store.id,
        "session_save_id": session1.id,
        "session_recall_id": session2.id,
        "saved_content": saved_content,
        "recalled_content": recalled,
        "cross_session_recall": bool(
            saved_content and recalled and saved_content == recalled,
        ),
    }
