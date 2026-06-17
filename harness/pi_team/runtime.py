"""Per-turn runtime for team tools."""

from __future__ import annotations

import asyncio
import os
from dataclasses import dataclass, field
from typing import Any


TEAM_TOOL_NAMES = frozenset(
    {
        "team_create",
        "spawn_teammate",
        "send_team_message",
        "read_team_messages",
    }
)


@dataclass
class TeamRuntime:
    session_id: str | None = None
    tenant_id: str | None = None
    platform_base: str | None = None
    internal_secret: str | None = None
    database_path: str | None = None
    lead_agent_id: str | None = None
    lead_member_id: str | None = None
    active_team_id: str | None = None
    enabled_tools: frozenset[str] = field(
        default_factory=lambda: frozenset(TEAM_TOOL_NAMES)
    )


_runtime: TeamRuntime | None = None


def configure_team_runtime(runtime: TeamRuntime) -> None:
    global _runtime
    _runtime = runtime


def get_team_runtime() -> TeamRuntime:
    if _runtime is not None:
        return _runtime
    return TeamRuntime(
        internal_secret=os.environ.get("OMA_INTERNAL_SECRET"),
        database_path=os.environ.get("OMA_DATABASE_PATH")
        or os.environ.get("DATABASE_PATH"),
    )


def clear_team_runtime() -> None:
    global _runtime
    if _runtime is not None and _runtime.session_id:
        from pi_team.loop import get_loop_manager

        loop = get_loop_manager()
        # Fire-and-forget; turn teardown should not block.
        try:
            asyncio.get_running_loop().create_task(
                loop.stop_all_for_session(_runtime.session_id)
            )
        except RuntimeError:
            pass
    _runtime = None
