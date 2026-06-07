"""OMA ↔ piPy DTO helpers."""

from __future__ import annotations

from typing import Any

from pydantic import BaseModel, Field


class AgentSnapshot(BaseModel):
    id: str
    name: str
    model: str
    system_prompt: str | None = None
    version: int = 1


class TurnRequest(BaseModel):
    session_id: str
    agent: AgentSnapshot
    events: list[dict[str, Any]] = Field(default_factory=list)
    workdir: str


class TurnResponse(BaseModel):
    events: list[dict[str, Any]] = Field(default_factory=list)
