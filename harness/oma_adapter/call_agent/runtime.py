"""Per-turn runtime for call_agent delegation."""

from __future__ import annotations

import asyncio
from contextvars import ContextVar, Token
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
    background_tasks: list[asyncio.Task[Any]] = field(
        default_factory=list,
        repr=False,
    )


_call_agent_runtime: ContextVar[CallAgentRuntime | None] = ContextVar(
    "call_agent_runtime",
    default=None,
)


def configure_call_agent(runtime: CallAgentRuntime) -> Token:
    return _call_agent_runtime.set(runtime)


def get_call_agent_runtime() -> CallAgentRuntime | None:
    return _call_agent_runtime.get()


def reset_call_agent_runtime(token: Token) -> None:
    _call_agent_runtime.reset(token)


def clear_call_agent_runtime() -> None:
    _call_agent_runtime.set(None)
