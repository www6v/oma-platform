"""OMA host bridge: wires pi_team runtime for harness turns."""

from __future__ import annotations

import json
from typing import Any
from urllib.parse import urljoin

import httpx

from oma_adapter.types import AgentSnapshot
from pi_team.loop import TeammateLoopManager
from pi_team.runtime import TEAM_TOOL_NAMES, TeamRuntime


class TeamPlatformError(RuntimeError):
    pass


async def _post_batch(runtime: TeamRuntime, body: dict[str, Any]) -> None:
    if not runtime.session_id:
        raise TeamPlatformError("team tools unavailable: session_id missing")
    base = (runtime.platform_base or "").rstrip("/")
    if not base:
        raise TeamPlatformError(
            "team tools unavailable: platform_base not configured"
        )
    secret = runtime.internal_secret or ""
    if not secret:
        raise TeamPlatformError(
            "team tools unavailable: OMA_INTERNAL_SECRET not configured"
        )
    url = urljoin(
        base + "/",
        f"v1/internal/sessions/{runtime.session_id}/events/batch",
    )
    headers = {
        "Content-Type": "application/json",
        "x-internal-secret": secret,
    }
    async with httpx.AsyncClient(timeout=60.0) as client:
        resp = await client.post(url, headers=headers, json=body)
    if resp.status_code >= 400:
        detail = resp.text.strip() or resp.reason_phrase
        try:
            payload = resp.json()
            if isinstance(payload, dict) and payload.get("error"):
                detail = str(payload["error"])
        except json.JSONDecodeError:
            pass
        raise TeamPlatformError(detail)


def build_team_runtime(
    *,
    session_id: str,
    tenant_id: str,
    platform_base: str | None,
    internal_secret: str | None,
    agent: AgentSnapshot,
    database_path: str | None = None,
) -> TeamRuntime | None:
    if not _needs_team_tools(agent):
        return None
    import os

    resolved_db = database_path or os.environ.get(
        "OMA_DATABASE_PATH"
    ) or os.environ.get("DATABASE_PATH")

    runtime = TeamRuntime(
        session_id=session_id,
        tenant_id=tenant_id,
        platform_base=platform_base,
        internal_secret=internal_secret,
        database_path=resolved_db,
        lead_agent_id=agent.id,
        enabled_tools=TEAM_TOOL_NAMES,
        loop_manager=TeammateLoopManager(),
    )

    async def append_session_events(events: list[dict[str, Any]]) -> None:
        if not events:
            return
        await _post_batch(runtime, {"events": events, "enqueue": False})

    async def enqueue_session_events(
        events: list[dict[str, Any]],
        *,
        run_turn: bool = True,
    ) -> None:
        if not events:
            return
        await _post_batch(
            runtime,
            {"events": events, "enqueue": True, "run_turn": run_turn},
        )

    runtime.append_session_events = append_session_events
    runtime.enqueue_session_events = enqueue_session_events
    return runtime


def _needs_team_tools(agent: AgentSnapshot) -> bool:
    if agent.metadata and agent.metadata.get("enable_team_tools") is True:
        return True
    for item in agent.tools or []:
        if not isinstance(item, dict):
            continue
        name = item.get("name")
        if isinstance(name, str) and name in TEAM_TOOL_NAMES:
            return True
    return False
