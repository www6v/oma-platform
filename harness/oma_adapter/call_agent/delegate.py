"""Delegate work to a sub-agent and return text for the parent tool result."""

from __future__ import annotations

import asyncio
import random
import time
from typing import Any

from oma_adapter.call_agent.roles import agent_snapshot_with_role
from oma_adapter.call_agent.runtime import get_call_agent_runtime
from oma_adapter.call_agent.sub_turn import extract_assistant_text, run_sub_agent_turn
from oma_adapter.types import AgentSnapshot

GENERAL_SUBAGENT_TOOLS = [
    {
        "type": "agent_toolset_20260401",
        "configs": [
            {"name": "bash", "enabled": True},
            {"name": "read", "enabled": True},
            {"name": "write", "enabled": True},
            {"name": "edit", "enabled": True},
            {"name": "grep", "enabled": True},
            {"name": "glob", "enabled": True},
            {"name": "web_fetch", "enabled": False},
        ],
    }
]

GENERAL_SYSTEM_PROMPT = (
    "You are a focused sub-agent. The user message contains a single task "
    "delegated to you by another agent. Do exactly that task and return a "
    "concise text result — no preamble, no follow-up questions, no offers "
    "to do additional work. You share the same sandbox as the calling agent "
    "(files persist) but cannot delegate further or use MCP tools."
)


def _thread_id() -> str:
    suffix = f"{int(time.time() * 1000):x}{random.randint(0, 0xFFFFFF):06x}"
    return f"sthr_{suffix}"


def _task_id() -> str:
    suffix = f"{int(time.time() * 1000):x}{random.randint(0, 0xFFFFFF):06x}"
    return f"sbtask_{suffix}"


def _general_subagent(parent: AgentSnapshot) -> AgentSnapshot:
    return AgentSnapshot(
        id="general",
        name="general",
        model=parent.model,
        system_prompt=GENERAL_SYSTEM_PROMPT,
        tools=GENERAL_SUBAGENT_TOOLS,
        version=1,
    )


def _apply_subagent_role(agent: AgentSnapshot) -> AgentSnapshot:
    if not agent.metadata:
        return agent
    role = agent.metadata.get("subagent_role")
    if not isinstance(role, str) or not role.strip():
        return agent
    return agent_snapshot_with_role(agent, role.strip())


def _resolve_sub_agent(agent_id: str, runtime) -> AgentSnapshot | None:
    if agent_id == "general":
        return _general_subagent(runtime.parent_agent)
    sub_agent = runtime.sub_agents.get(agent_id)
    if sub_agent is None:
        return None
    return _apply_subagent_role(sub_agent)


async def _emit(runtime, event: dict[str, Any]) -> None:
    if runtime.emit_event is not None:
        await runtime.emit_event(event)


async def _run_delegate_body(
    *,
    runtime,
    agent_id: str,
    sub_agent: AgentSnapshot,
    message: str,
    thread_id: str,
    task_id: str | None = None,
) -> str:
    try:
        resp = await run_sub_agent_turn(
            session_id=runtime.session_id,
            tenant_id=runtime.tenant_id,
            agent=sub_agent,
            message=message,
            workdir=runtime.workdir,
            model=runtime.model,
            aux_model=runtime.aux_model,
            environment=runtime.environment,
            thread_id=thread_id,
            mcp_proxy_base=runtime.mcp_proxy_base,
            mcp_proxy_api_key=runtime.mcp_proxy_api_key,
            outbound_proxy_addr=runtime.outbound_proxy_addr,
            outbound_proxy_api_key=runtime.outbound_proxy_api_key,
            sub_agents=runtime.sub_agents,
            parent_agent=runtime.parent_agent,
            depth=runtime.depth + 1,
            on_event=runtime.emit_event,
        )
    except Exception as exc:  # noqa: BLE001 — surfaced to parent model as tool text
        return f"Sub-agent error: {exc}"

    text = extract_assistant_text(resp.events)
    await _emit(
        runtime,
        {
            "type": "session.thread_idle",
            "session_thread_id": thread_id,
        },
    )
    if task_id is not None:
        summary = text[:500] if text else "Sub-agent completed with no text output"
        await _emit(
            runtime,
            {
                "type": "session.sub_agent_completed",
                "task_id": task_id,
                "session_thread_id": thread_id,
                "agent_id": agent_id,
                "summary": summary,
            },
        )
    if text:
        return text
    return "Sub-agent completed with no text output"


async def _background_delegate(
    *,
    runtime,
    agent_id: str,
    sub_agent: AgentSnapshot,
    message: str,
    thread_id: str,
    task_id: str,
) -> None:
    try:
        await _run_delegate_body(
            runtime=runtime,
            agent_id=agent_id,
            sub_agent=sub_agent,
            message=message,
            thread_id=thread_id,
            task_id=task_id,
        )
    except Exception as exc:  # noqa: BLE001
        await _emit(
            runtime,
            {
                "type": "session.sub_agent_completed",
                "task_id": task_id,
                "session_thread_id": thread_id,
                "agent_id": agent_id,
                "summary": f"Sub-agent error: {exc}",
                "is_error": True,
            },
        )
        await _emit(
            runtime,
            {
                "type": "session.thread_idle",
                "session_thread_id": thread_id,
            },
        )


async def delegate_to_agent(
    agent_id: str,
    message: str,
    *,
    run_in_background: bool = False,
) -> str:
    runtime = get_call_agent_runtime()
    if runtime is None:
        return "Multi-agent delegation not available: no thread executor configured"
    if runtime.depth >= runtime.max_depth:
        return (
            f"Sub-agent error: delegation depth limit ({runtime.max_depth}) reached"
        )

    sub_agent = _resolve_sub_agent(agent_id, runtime)
    if sub_agent is None:
        return f'Sub-agent error: agent "{agent_id}" not found'

    thread_id = _thread_id()
    await _emit(
        runtime,
        {
            "type": "session.thread_created",
            "session_thread_id": thread_id,
            "agent_id": agent_id,
            "agent_name": sub_agent.name,
            "parent_thread_id": runtime.parent_thread_id,
        },
    )

    if run_in_background:
        task_id = _task_id()
        await _emit(
            runtime,
            {
                "type": "session.sub_agent_started",
                "task_id": task_id,
                "session_thread_id": thread_id,
                "agent_id": agent_id,
                "agent_name": sub_agent.name,
            },
        )
        bg = asyncio.create_task(
            _background_delegate(
                runtime=runtime,
                agent_id=agent_id,
                sub_agent=sub_agent,
                message=message,
                thread_id=thread_id,
                task_id=task_id,
            )
        )
        runtime.background_tasks.append(bg)
        return (
            f"Background sub-agent started. task_id={task_id} "
            f"thread_id={thread_id} agent_id={agent_id}. "
            "You will be notified when it completes."
        )

    return await _run_delegate_body(
        runtime=runtime,
        agent_id=agent_id,
        sub_agent=sub_agent,
        message=message,
        thread_id=thread_id,
    )
