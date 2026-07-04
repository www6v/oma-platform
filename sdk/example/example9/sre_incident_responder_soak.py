"""SRE live soak runner — incident responder cookbook."""

from __future__ import annotations

import os
from typing import Any

from oma_sdk import OMAClient, StreamConfig, stream_hitl_until_end_turn, wait_for_idle_status

from sre_fixtures import (
    AGENT_NAME,
    AGENT_SYSTEM,
    AGENT_TOOLS,
    ENV_CONFIG,
    ENV_NAME,
    SESSION_TITLE,
    SKILL_RUNBOOK_BODY,
    build_session_resources,
    fixture_path,
    load_alert_json,
)


def _stream_config() -> StreamConfig:
    return StreamConfig(
        timeout_sec=float(os.getenv("OMA_SRE_TIMEOUT_SEC", "900")),
        stream_read_timeout=float(
            os.getenv("OMA_STREAM_READ_TIMEOUT_SEC", "300"),
        ),
        idle_poll_max_wait=float(
            os.getenv("OMA_IDLE_POLL_MAX_WAIT_SEC", "120"),
        ),
    )


async def _create_skill(
    client: OMAClient,
    *,
    display_title: str,
    description: str | None = None,
    files: list[tuple[str, str | bytes]],
) -> dict[str, Any]:
    """Create a skill via OMA JSON POST /v1/skills."""
    file_payloads: list[dict[str, str]] = []
    for filename, content in files:
        text = content.decode("utf-8") if isinstance(content, bytes) else content
        file_payloads.append({"filename": filename, "content": text})

    body: dict[str, Any] = {
        "display_title": display_title,
        "files": file_payloads,
    }
    if description:
        body["description"] = description

    resp = await client._http.post("/v1/skills", json=body)
    resp.raise_for_status()
    return resp.json()


def _make_sre_custom_tool_handler(pr_state: dict[str, Any]):
    """Answer open_pull_request and merge_pull_request inline; pause on approval."""

    def handle_custom_tool(name: str, args: dict[str, Any]) -> dict[str, Any] | None:
        if name == "open_pull_request":
            pr_state["pr_number"] = pr_state.get("pr_number", 1)
            pr_state["title"] = args.get("title", "")
            return {
                "pr_number": pr_state["pr_number"],
                "url": f"https://github.test/pr/{pr_state['pr_number']}",
            }
        if name == "merge_pull_request":
            pr_state["merged"] = True
            pr_state["pr_number"] = args.get("pr_number", pr_state.get("pr_number", 1))
            return {"merged": True, "pr_number": pr_state["pr_number"]}
        if name == "request_approval":
            return None
        return {"error": f"unknown tool {name}"}

    return handle_custom_tool


async def run_sre_incident_responder_soak(
    client: OMAClient,
    *,
    model: str,
    keep_resources: bool = False,
    auto_approve: bool = True,
) -> dict[str, Any]:
    """Run SRE cookbook: skill + 3 mounts + PagerDuty alert + HITL approval."""
    skill = await _create_skill(
        client,
        display_title="Incident Runbooks",
        description="Consult runbooks before infra changes",
        files=[("SKILL.md", SKILL_RUNBOOK_BODY)],
    )
    skill_version = skill.get("latest_version") or skill.get("version", "")

    log_upload = await client.files.upload(
        file=(
            "checkout-svc.log",
            fixture_path("logs/checkout-svc.log").read_bytes(),
            "text/plain",
        ),
        downloadable=True,
    )
    manifest_upload = await client.files.upload(
        file=(
            "checkout-deploy.yaml",
            fixture_path("infra/k8s/checkout-deploy.yaml").read_bytes(),
            "text/yaml",
        ),
        downloadable=True,
    )
    runbook_upload = await client.files.upload(
        file=(
            "oom.md",
            fixture_path("runbooks/oom.md").read_bytes(),
            "text/markdown",
        ),
        downloadable=True,
    )

    agent = client.agents.create(
        name=AGENT_NAME,
        model={"id": model},
        system=AGENT_SYSTEM,
        tools=AGENT_TOOLS,
        skills=[
            {
                "type": "custom",
                "skill_id": skill["id"],
                "version": skill_version,
            }
        ],
    )
    env = client.environments.create(name=ENV_NAME, config=ENV_CONFIG)
    session = client.sessions.create(
        environment_id=env.id,
        agent={"type": "agent", "id": agent.id, "version": agent.version},
        title=SESSION_TITLE,
        resources=build_session_resources(
            log_upload["id"],
            manifest_upload["id"],
            runbook_upload["id"],
        ),
    )

    pr_state: dict[str, Any] = {}
    cfg = _stream_config()
    alert_text = "PagerDuty alert:\n" + load_alert_json()

    async def on_custom_tool(
        name: str,
        _event_id: str,
        args: dict[str, Any],
    ) -> dict[str, Any] | None:
        handler = _make_sre_custom_tool_handler(pr_state)
        result = handler(name, args)
        if result is None and name == "request_approval" and auto_approve:
            return {"decision": "approved"}
        return result

    hitl_state = await stream_hitl_until_end_turn(
        client,
        session.id,
        send_events=[
            {
                "type": "user.message",
                "content": [{"type": "text", "text": alert_text}],
            },
        ],
        on_custom_tool=on_custom_tool,
        config=cfg,
    )
    await wait_for_idle_status(client, session.id, max_wait=cfg.idle_poll_max_wait)

    if not keep_resources and os.getenv("OMA_KEEP_RESOURCES") != "1":
        client.sessions.archive(session.id)
        client.agents.archive(agent.id)
        client.environments.archive(env.id)

    return {
        "session_id": session.id,
        "skill_id": skill["id"],
        "turn_count": hitl_state.get("turn_count", 1),
        "pr_state": pr_state,
        "responded_ids": list(hitl_state.get("responded_ids", [])),
    }
