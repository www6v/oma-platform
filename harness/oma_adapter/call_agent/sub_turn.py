"""Run an isolated sub-agent turn and tag emitted events."""

from __future__ import annotations

from typing import Any, Awaitable, Callable

from oma_adapter.types import AgentSnapshot, ModelConfig, TurnResponse

EventCallback = Callable[[dict[str, Any]], Awaitable[None]]


def _strip_delegation(agent: AgentSnapshot) -> AgentSnapshot:
    """Sub-agents at depth>0 should not re-register the same roster."""
    data = agent.model_dump()
    data["callable_agents"] = None
    return AgentSnapshot.model_validate(data)


async def run_sub_agent_turn(
    *,
    session_id: str,
    tenant_id: str | None,
    agent: AgentSnapshot,
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
    sub_agents: dict[str, AgentSnapshot],
    parent_agent: AgentSnapshot,
    depth: int,
    on_event: EventCallback | None,
) -> TurnResponse:
    from oma_adapter.turn import _run_turn_core

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

    from oma_adapter.call_agent.runtime import (
        CallAgentRuntime,
        configure_call_agent,
        clear_call_agent_runtime,
    )

    configure_call_agent(
        CallAgentRuntime(
            session_id=session_id,
            tenant_id=tenant_id,
            workdir=workdir,
            parent_agent=parent_agent,
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
        return await _run_turn_core(
            session_id=session_id,
            tenant_id=tenant_id,
            agent=_strip_delegation(agent),
            model=model,
            aux_model=aux_model,
            environment=environment,
            events=events,
            workdir=workdir,
            mcp_proxy_base=mcp_proxy_base,
            mcp_proxy_api_key=mcp_proxy_api_key,
            outbound_proxy_addr=outbound_proxy_addr,
            outbound_proxy_api_key=outbound_proxy_api_key,
            create_session=None,
            on_event=tagged_on_event,
        )
    finally:
        clear_call_agent_runtime()


def extract_assistant_text(events: list[dict[str, Any]]) -> str:
    for event in reversed(events):
        if event.get("type") != "agent.message":
            continue
        parts: list[str] = []
        for block in event.get("content") or []:
            if block.get("type") == "text" and block.get("text"):
                parts.append(str(block["text"]))
        text = "\n".join(parts).strip()
        if text:
            return text
    return ""
