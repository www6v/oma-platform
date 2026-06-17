"""Sub-agent DTOs — host-agnostic agent snapshots for delegation."""

from __future__ import annotations

from typing import Any

from pydantic import BaseModel, Field


class CallableAgentRef(BaseModel):
    type: str = "agent"
    id: str
    version: int = 1


class SubAgentSnapshot(BaseModel):
    id: str
    name: str
    model: str
    aux_model: str | None = None
    system: str | None = None
    system_prompt: str | None = None
    description: str | None = None
    tools: list[dict[str, Any]] | None = None
    mcp_servers: list[dict[str, Any]] | None = None
    callable_agents: list[CallableAgentRef] | None = None
    metadata: dict[str, Any] | None = None
    version: int = 1

    @property
    def enable_general_subagent(self) -> bool:
        if self.metadata and self.metadata.get("enable_general_subagent") is True:
            return True
        return False


class SubTurnResult(BaseModel):
    events: list[dict[str, Any]] = Field(default_factory=list)
