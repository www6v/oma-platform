"""OMA host bridge: wires pi_subagent runtime to harness turn execution."""

from __future__ import annotations

from typing import Any, Awaitable, Callable

from oma_adapter.types import AgentSnapshot, ModelConfig, TurnResponse
from pi_subagent.runtime import SubAgentRuntime, RunSubTurnFn
from pi_subagent.types import SubAgentSnapshot, SubTurnResult

EventCallback = Callable[[dict[str, Any]], Awaitable[None]]


def _to_subagent_snapshot(agent: AgentSnapshot) -> SubAgentSnapshot:
    return SubAgentSnapshot.model_validate(agent.model_dump())


async def _oma_run_sub_turn(
    *,
    session_id: str,
    tenant_id: str | None,
    agent: SubAgentSnapshot,
    message: str,
    workdir: str,
    model: ModelConfig | None,
    aux_model: ModelConfig | None,
    environment: dict[str, Any] | None,
    thread_id: str,
    mcp_proxy_base: str | None,
    mcp_proxy_api_key: str | None,
    outbound_proxy_addr: str | None,
    outbound_proxy_api_key: str | None,
    sub_agents: dict[str, SubAgentSnapshot],
    parent_agent: SubAgentSnapshot,
    depth: int,
    on_event: EventCallback | None,
) -> SubTurnResult:
    from oma_adapter.turn import _run_turn_core
    from pi_subagent.runtime import (
        SubAgentRuntime as Runtime,
        configure_subagent_runtime,
        reset_subagent_runtime,
    )

    events = [
        {
            "type": "user.message",
            "content": [{"type": "text", "text": message}],
        }
    ]

    async def tagged_on_event(event: dict[str, Any]) -> None:
        tagged = dict(event)
        tagged["session_thread_id"] = thread_id
        if on_event is not None:
            await on_event(tagged)

    sub_oma = {
        aid: AgentSnapshot.model_validate(s.model_dump())
        for aid, s in sub_agents.items()
    }

    token = configure_subagent_runtime(
        Runtime(
            session_id=session_id,
            tenant_id=tenant_id,
            workdir=workdir,
            parent_agent=parent_agent,
            run_sub_turn=_oma_run_sub_turn,
            sub_agents=sub_agents,
            model=model,
            aux_model=aux_model,
            environment=environment,
            emit_event=tagged_on_event,
            mcp_proxy_base=mcp_proxy_base,
            mcp_proxy_api_key=mcp_proxy_api_key,
            outbound_proxy_addr=outbound_proxy_addr,
            outbound_proxy_api_key=outbound_proxy_api_key,
            parent_thread_id=thread_id,
            depth=depth,
        )
    )
    try:
        resp: TurnResponse = await _run_turn_core(
            session_id=session_id,
            tenant_id=tenant_id,
            agent=_strip_delegation_to_oma(agent),
            model=model,
            aux_model=aux_model,
            environment=environment,
            events=events,
            workdir=workdir,
            mcp_proxy_base=mcp_proxy_base,
            mcp_proxy_api_key=mcp_proxy_api_key,
            outbound_proxy_addr=outbound_proxy_addr,
            outbound_proxy_api_key=outbound_proxy_api_key,
            sub_agents=sub_oma,
            create_session=None,
            on_event=tagged_on_event,
        )
        return SubTurnResult(events=resp.events)
    finally:
        reset_subagent_runtime(token)


def _strip_delegation_to_oma(agent: SubAgentSnapshot) -> AgentSnapshot:
    data = agent.model_dump()
    data["callable_agents"] = None
    return AgentSnapshot.model_validate(data)


def build_subagent_runtime(
    *,
    session_id: str,
    tenant_id: str | None,
    workdir: str,
    parent_agent: AgentSnapshot,
    sub_agents: dict[str, AgentSnapshot] | None,
    model: ModelConfig | None,
    aux_model: ModelConfig | None,
    environment: dict[str, Any] | None,
    emit_event: EventCallback | None,
    mcp_proxy_base: str | None,
    mcp_proxy_api_key: str | None,
    outbound_proxy_addr: str | None,
    outbound_proxy_api_key: str | None,
    run_sub_turn: RunSubTurnFn | None = None,
) -> SubAgentRuntime:
    parent = _to_subagent_snapshot(parent_agent)
    subs = {
        aid: _to_subagent_snapshot(s)
        for aid, s in (sub_agents or {}).items()
    }
    runner = run_sub_turn or _oma_run_sub_turn
    return SubAgentRuntime(
        session_id=session_id,
        tenant_id=tenant_id,
        workdir=workdir,
        parent_agent=parent,
        run_sub_turn=runner,
        sub_agents=subs,
        model=model,
        aux_model=aux_model,
        environment=environment,
        emit_event=emit_event,
        mcp_proxy_base=mcp_proxy_base,
        mcp_proxy_api_key=mcp_proxy_api_key,
        outbound_proxy_addr=outbound_proxy_addr,
        outbound_proxy_api_key=outbound_proxy_api_key,
    )
