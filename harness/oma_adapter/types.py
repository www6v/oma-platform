"""OMA ↔ piPy DTO helpers."""

from __future__ import annotations

from typing import Any

from pydantic import BaseModel, Field


class ModelConfig(BaseModel):
    model: str
    provider: str | None = None
    api_key: str | None = None
    base_url: str | None = None
    custom_headers: dict[str, str] | None = None


class AgentSnapshot(BaseModel):
    id: str
    name: str
    model: str
    aux_model: str | None = None
    system_prompt: str | None = None
    description: str | None = None
    tools: list[dict[str, Any]] | None = None
    mcp_servers: list[dict[str, Any]] | None = None
    version: int = 1


class TurnRequest(BaseModel):
    session_id: str
    tenant_id: str | None = None
    agent: AgentSnapshot
    model: ModelConfig | None = None
    aux_model: ModelConfig | None = None
    environment: dict[str, Any] | None = None
    events: list[dict[str, Any]] = Field(default_factory=list)
    workdir: str
    mcp_proxy_base: str | None = None
    mcp_proxy_api_key: str | None = None
    outbound_proxy_addr: str | None = None
    outbound_proxy_api_key: str | None = None


class TurnResponse(BaseModel):
    events: list[dict[str, Any]] = Field(default_factory=list)
