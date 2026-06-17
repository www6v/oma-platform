"""OMA host bridge: wires pi_team runtime for harness turns."""

from __future__ import annotations

from oma_adapter.types import AgentSnapshot
from pi_team.runtime import TEAM_TOOL_NAMES, TeamRuntime


def build_team_runtime(
    *,
    session_id: str,
    tenant_id: str,
    platform_base: str | None,
    internal_secret: str | None,
    agent: AgentSnapshot,
) -> TeamRuntime | None:
    if not _needs_team_tools(agent):
        return None
    import os

    return TeamRuntime(
        session_id=session_id,
        tenant_id=tenant_id,
        platform_base=platform_base,
        internal_secret=internal_secret,
        database_path=os.environ.get("OMA_DATABASE_PATH")
        or os.environ.get("DATABASE_PATH"),
        lead_agent_id=agent.id,
        enabled_tools=TEAM_TOOL_NAMES,
    )


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
