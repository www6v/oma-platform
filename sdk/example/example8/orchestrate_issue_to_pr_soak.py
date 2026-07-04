"""OR live soak runner — orchestrate issue to PR cookbook."""

from __future__ import annotations

import os
from typing import Any

from oma_sdk import OMAClient, StreamConfig, stream_until_end_turn, wait_for_idle_status

from orchestrate_fixtures import (
    AGENT_NAME,
    AGENT_SYSTEM,
    AGENT_TOOLS,
    CHAIN_USER_MESSAGE,
    ENV_CONFIG,
    ENV_NAME,
    SESSION_TITLE,
    VERIFY_USER_MESSAGE,
    build_repo_resource,
    make_orchestrate_repo_zip,
)


def _stream_config() -> StreamConfig:
    return StreamConfig(
        timeout_sec=float(os.getenv("OMA_ORCHESTRATE_TIMEOUT_SEC", "900")),
        stream_read_timeout=float(
            os.getenv("OMA_STREAM_READ_TIMEOUT_SEC", "300"),
        ),
        idle_poll_max_wait=float(
            os.getenv("OMA_IDLE_POLL_MAX_WAIT_SEC", "120"),
        ),
    )


async def run_orchestrate_issue_to_pr_soak(
    client: OMAClient,
    *,
    model: str,
    keep_resources: bool = False,
) -> dict[str, Any]:
    """Run orchestrate cookbook: zip mock gh + chain turn + verify turn."""
    repo_upload = await client.files.upload(
        file=("repo.zip", make_orchestrate_repo_zip(), "application/zip"),
        downloadable=True,
    )
    repo_file_id = repo_upload["id"]

    agent = client.agents.create(
        name=AGENT_NAME,
        model={"id": model},
        system=AGENT_SYSTEM,
        tools=AGENT_TOOLS,
    )
    env = client.environments.create(name=ENV_NAME, config=ENV_CONFIG)
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
                "content": [{"type": "text", "text": CHAIN_USER_MESSAGE}],
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
                "content": [{"type": "text", "text": VERIFY_USER_MESSAGE}],
            },
        ],
        config=cfg,
    )
    await wait_for_idle_status(client, session.id, max_wait=cfg.idle_poll_max_wait)

    if not keep_resources and os.getenv("OMA_KEEP_RESOURCES") != "1":
        client.sessions.archive(session.id)
        client.agents.archive(agent.id)
        client.environments.archive(env.id)

    return {
        "session_id": session.id,
        "repo_file_id": repo_file_id,
        "turn_count": 2,
    }
