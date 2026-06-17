"""Per-turn runtime for sub-agent delegation (ContextVar isolation)."""

from __future__ import annotations

import asyncio
from contextvars import ContextVar, Token
from dataclasses import dataclass, field
from typing import Any, Awaitable, Callable

from pi_subagent.types import SubAgentSnapshot, SubTurnResult

EmitEventFn = Callable[[dict[str, Any]], Awaitable[None]]
RunSubTurnFn = Callable[..., Awaitable[SubTurnResult]]


@dataclass
class SubAgentRuntime:
    session_id: str
    workdir: str
    parent_agent: SubAgentSnapshot
    run_sub_turn: RunSubTurnFn
    tenant_id: str | None = None
    sub_agents: dict[str, SubAgentSnapshot] = field(default_factory=dict)
    model: Any | None = None
    aux_model: Any | None = None
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


_runtime: ContextVar[SubAgentRuntime | None] = ContextVar(
    "subagent_runtime",
    default=None,
)


def configure_subagent_runtime(runtime: SubAgentRuntime) -> Token:
    return _runtime.set(runtime)


def get_subagent_runtime() -> SubAgentRuntime | None:
    return _runtime.get()


def reset_subagent_runtime(token: Token) -> None:
    _runtime.reset(token)


def clear_subagent_runtime() -> None:
    _runtime.set(None)
