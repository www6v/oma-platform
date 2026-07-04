"""EX live soak runner — explore unfamiliar codebase cookbook."""

from __future__ import annotations

import os
from typing import Any

from oma_sdk import OMAClient, StreamConfig, stream_until_end_turn, wait_for_idle_status

from explore_fixtures import (
    AGENT_NAME,
    AGENT_SYSTEM,
    AGENT_TOOLS,
    DEPLOY_FOLLOWUP_MESSAGE,
    DEPLOY_HISTORY_BYTES,
    DEPLOY_HISTORY_MOUNT,
    ENV_NAME,
    EXPLORE_USER_MESSAGE,
    NOTES_USER_MESSAGE,
    SESSION_TITLE,
    build_repo_resource,
    make_unfamiliar_repo_zip,
)


def _stream_config() -> StreamConfig:
    return StreamConfig(
        timeout_sec=float(os.getenv("OMA_EXPLORE_TIMEOUT_SEC", "600")),
        stream_read_timeout=float(
            os.getenv("OMA_STREAM_READ_TIMEOUT_SEC", "300"),
        ),
        idle_poll_max_wait=float(
            os.getenv("OMA_IDLE_POLL_MAX_WAIT_SEC", "60"),
        ),
    )


async def run_explore_unfamiliar_codebase_soak(
    client: OMAClient,
    *,
    model: str,
    keep_resources: bool = False,
) -> dict[str, Any]:
    """Run explore cookbook: zip mount, turns, mid-session resources.add/delete."""
    repo_upload = await client.files.upload(
        file=("repo.zip", make_unfamiliar_repo_zip(), "application/zip"),
        downloadable=True,
    )
    repo_file_id = repo_upload["id"]

    deploy_upload = await client.files.upload(
        filename="DEPLOY_HISTORY.md",
        content=DEPLOY_HISTORY_BYTES,
        media_type="text/markdown",
        downloadable=True,
    )
    deploy_file_id = deploy_upload["id"]

    agent = client.agents.create(
        name=AGENT_NAME,
        model={"id": model},
        system=AGENT_SYSTEM,
        tools=AGENT_TOOLS,
    )
    env = client.environments.create(
        name=ENV_NAME,
        config={"type": "cloud", "networking": {"type": "limited"}},
    )
    session = client.sessions.create(
        environment_id=env.id,
        agent={"type": "agent", "id": agent.id, "version": agent.version},
        title=SESSION_TITLE,
        resources=[build_repo_resource(repo_file_id)],
    )
    cfg = _stream_config()

    await stream_until_end_turn(
        client,
        session.id,
        send_events=[
            {
                "type": "user.message",
                "content": [{"type": "text", "text": EXPLORE_USER_MESSAGE}],
            },
        ],
        config=cfg,
    )
    await wait_for_idle_status(client, session.id, max_wait=cfg.idle_poll_max_wait)

    await stream_until_end_turn(
        client,
        session.id,
        send_events=[
            {
                "type": "user.message",
                "content": [{"type": "text", "text": NOTES_USER_MESSAGE}],
            },
        ],
        config=cfg,
    )
    await wait_for_idle_status(client, session.id, max_wait=cfg.idle_poll_max_wait)

    added = client.sessions.resources.add(
        session.id,
        type="file",
        file_id=deploy_file_id,
        mount_path=DEPLOY_HISTORY_MOUNT,
    )
    listed = client.sessions.resources.list(session.id)
    resource_count_after_add = len(getattr(listed, "data", listed))

    await stream_until_end_turn(
        client,
        session.id,
        send_events=[
            {
                "type": "user.message",
                "content": [{"type": "text", "text": DEPLOY_FOLLOWUP_MESSAGE}],
            },
        ],
        config=cfg,
    )
    await wait_for_idle_status(client, session.id, max_wait=cfg.idle_poll_max_wait)

    client.sessions.resources.delete(added.id, session_id=session.id)
    listed_after = client.sessions.resources.list(session.id)
    resource_count_after_delete = len(getattr(listed_after, "data", listed_after))

    if not keep_resources and os.getenv("OMA_KEEP_RESOURCES") != "1":
        client.sessions.archive(session.id)
        client.agents.archive(agent.id)
        client.environments.archive(env.id)

    return {
        "session_id": session.id,
        "repo_file_id": repo_file_id,
        "deploy_resource_id": added.id,
        "resource_count_after_add": resource_count_after_add,
        "resource_count_after_delete": resource_count_after_delete,
    }
