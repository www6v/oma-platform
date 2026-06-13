"""Per-turn runtime for call_agent delegation."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any, Awaitable, Callable

from oma_adapter.types import AgentSnapshot, ModelConfig

EmitEventFn = Callable[[dict[str, Any]], Awaitable[None]]


@dataclass
class CallAgentRuntime:
    session_id: str
    workdir: str
    parent_agent: AgentSnapshot
    tenant_id: str | None = None
    sub_agents: dict[str, AgentSnapshot] = field(default_factory=dict)
    model: ModelConfig | None = None
    aux_model: ModelConfig | None = None
    environment: dict[str, Any] | None = None
    emit_event: EmitEventFn | None = None
    mcp_proxy_base: str | None = None
    mcp_proxy_api_key: str | None = None
    outbound_proxy_addr: str | None = None
    outbound_proxy_api_key: str | None = None
    parent_thread_id: str = "sthr_primary"
    depth: int = 0
    max_depth: int = 3


_runtime: CallAgentRuntime | None = None


def configure_call_agent(runtime: CallAgentRuntime) -> None:
    global _runtime
    _runtime = runtime


def get_call_agent_runtime() -> CallAgentRuntime | None:
    return _runtime


def clear_call_agent_runtime() -> None:
    global _runtime
    _runtime = None
