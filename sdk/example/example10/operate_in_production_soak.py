"""Operate in production live soak runner."""

from __future__ import annotations

import os
from typing import Any

from oma_sdk import OMAClient, StreamConfig, stream_until_end_turn, wait_for_idle_status

from operate_fixtures import (
    AGENT_NAME,
    AGENT_SYSTEM,
    ENV_CONFIG,
    ENV_NAME,
    GITHUB_MCP_SERVER_NAME,
    SESSION_TITLE,
)


def _stream_config() -> StreamConfig:
    return StreamConfig(
        timeout_sec=float(os.getenv("OMA_OPERATE_TIMEOUT_SEC", "600")),
        stream_read_timeout=float(
            os.getenv("OMA_STREAM_READ_TIMEOUT_SEC", "300"),
        ),
        idle_poll_max_wait=float(
            os.getenv("OMA_IDLE_POLL_MAX_WAIT_SEC", "120"),
        ),
    )


async def run_operate_in_production_soak(
    client: OMAClient,
    *,
    model: str,
    mcp_server_url: str,
    github_token: str,
    keep_resources: bool = False,
) -> dict[str, Any]:
    """Run operate cookbook: vault + credential + vault_ids session + MCP turn."""
    vault_resp = await client._http.post(
        "/v1/vaults",
        json={"name": "operate-user-vault"},
    )
    vault_resp.raise_for_status()
    vault_id = vault_resp.json()["id"]

    cred_resp = await client._http.post(
        f"/v1/vaults/{vault_id}/credentials",
        json={
            "display_name": "GitHub MCP",
            "auth": {
                "type": "static_bearer",
                "mcp_server_url": mcp_server_url,
                "token": github_token,
            },
        },
    )
    cred_resp.raise_for_status()

    agent = client.agents.create(
        name=AGENT_NAME,
        model={"id": model},
        system=AGENT_SYSTEM,
        mcp_servers=[
            {
                "name": GITHUB_MCP_SERVER_NAME,
                "type": "url",
                "url": mcp_server_url,
            },
        ],
        tools=[
            {
                "type": "mcp_toolset",
                "mcp_server_name": GITHUB_MCP_SERVER_NAME,
                "default_config": {
                    "permission_policy": {"type": "always_allow"},
                },
            },
        ],
    )
    env = client.environments.create(name=ENV_NAME, config=ENV_CONFIG)
    session = client.sessions.create(
        environment_id=env.id,
        agent={"type": "agent", "id": agent.id, "version": agent.version},
        title=SESSION_TITLE,
        vault_ids=[vault_id],
    )

    cfg = _stream_config()
    await stream_until_end_turn(
        client,
        session.id,
        send_events=[{
            "type": "user.message",
            "content": [{
                "type": "text",
                "text": (
                    "Use the github MCP toolset to list my repositories "
                    "(limit 3) and summarize what you find."
                ),
            }],
        }],
        config=cfg,
    )
    await wait_for_idle_status(client, session.id, max_wait=cfg.idle_poll_max_wait)
    retrieved = client.sessions.retrieve(session.id)
    assert list(retrieved.vault_ids or []) == [vault_id]

    if not keep_resources:
        client.sessions.archive(session.id)
        client.agents.archive(agent.id)
        client.environments.archive(env.id)
        await client._http.post(f"/v1/vaults/{vault_id}/archive")

    print(
        "Part B (webhooks / session.status_idled): deferred — "
        "use wait_for_idle_status / SSE poll; "
        "see operate-in-production-migration.md",
    )

    return {
        "session_id": session.id,
        "vault_id": vault_id,
        "vault_ids": list(retrieved.vault_ids or []),
    }
