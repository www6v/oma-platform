"""Stateless harness turn via piPy SDK."""

from __future__ import annotations

import asyncio
import os
from pathlib import Path
from typing import Any, Awaitable, Callable

from oma_adapter.compaction import (
    compact_events,
    resolve_context_window_tokens,
    should_compact,
)
from oma_adapter.call_agent.runtime import (
    CallAgentRuntime,
    clear_call_agent_runtime,
    configure_call_agent,
)
from oma_adapter.emit import emit_oma_events
from oma_adapter.platform_guidance import compose_system_prompt
from oma_adapter.project import project_oma_events
from oma_adapter.provider_env import provider_env
from oma_adapter.sandbox_paths import patch_path_utils
from oma_adapter.outbound.setup import (
    clear_outbound_proxy_for_turn,
    normalize_outbound_proxy_addr,
    setup_outbound_proxy_for_turn,
)
from oma_adapter.mcp.runtime import clear_mcp_runtime
from oma_adapter.mcp.setup import mcp_servers_from_agent, setup_mcp_runtime_for_turn
from oma_adapter.tools import session_tool_config_from_agent
from oma_adapter.types import AgentSnapshot, ModelConfig, TurnResponse
from oma_adapter.web_fetch.runtime import WebFetchRuntime, clear_web_fetch_runtime, configure_web_fetch

CreateSessionFn = Callable[[Any], Awaitable[Any]]
EventCallback = Callable[[dict[str, Any]], Awaitable[None]]


def _assistant_text_from_session(session: Any) -> str | None:
    getter = getattr(session, "get_last_assistant_text", None)
    if callable(getter):
        text = getter()
        if isinstance(text, str) and text.strip():
            return text.strip()

    legacy = getattr(session, "last_assistant_text", None)
    if callable(legacy):
        text = legacy()
        if isinstance(text, str) and text.strip():
            return text.strip()
    if isinstance(legacy, str) and legacy.strip():
        return legacy.strip()
    return None


def _collect_pi_event(buffer: list[dict[str, Any]], event: Any) -> None:
    if hasattr(event, "type"):
        from pi_coding_agent.modes.json_mode import agent_event_to_dict

        buffer.append(agent_event_to_dict(event))
        return
    if isinstance(event, dict):
        buffer.append(event)


def _make_event_listener(
    buffer: list[dict[str, Any]],
) -> Callable[[Any], None]:
    def listener(event: Any) -> None:
        _collect_pi_event(buffer, event)

    return listener


async def _default_create_session(
    *,
    workdir: str,
    model: str,
    system_prompt: str | None,
    builtin_tools: list[str],
    extension_paths: list[str],
) -> Any:
    from pi_coding_agent.sdk import CreateAgentSessionOptions, create_agent_session

    opts = CreateAgentSessionOptions(
        cwd=Path(workdir),
        model=model,
        system_prompt=system_prompt,
        tools=builtin_tools,
        extension_paths=extension_paths or None,
        in_memory=True,
    )
    return await create_agent_session(opts)


async def _run_turn_core(
    *,
    session_id: str,
    tenant_id: str | None = None,
    agent: AgentSnapshot,
    sub_agents: dict[str, AgentSnapshot] | None = None,
    model: ModelConfig | None,
    aux_model: ModelConfig | None = None,
    environment: dict[str, Any] | None = None,
    events: list[dict[str, Any]],
    workdir: str,
    mcp_proxy_base: str | None = None,
    mcp_proxy_api_key: str | None = None,
    outbound_proxy_addr: str | None = None,
    outbound_proxy_api_key: str | None = None,
    create_session: CreateSessionFn | None,
    on_event: EventCallback | None,
) -> TurnResponse:

    working_events = list(events)
    context_window = resolve_context_window_tokens(
        model.model if model is not None else agent.model,
    )
    summarize_cfg = aux_model if aux_model is not None else model
    if summarize_cfg is not None and should_compact(
        working_events,
        context_window_tokens=context_window,
    ):
        boundary = await compact_events(
            working_events,
            model_cfg=summarize_cfg,
            context_window_tokens=context_window,
        )
        if boundary is not None:
            working_events.append(boundary)
            if on_event is not None:
                await on_event(boundary)

    prompt = project_oma_events(working_events)
    if not prompt:
        return TurnResponse(events=[])

    resolved_model = model.model if model is not None else agent.model
    if not resolved_model.startswith("faux/") and os.environ.get("OMA_FAKE_HARNESS") == "1":
        resolved_model = "faux/test"

    patch_path_utils(workdir)

    outbound_host = normalize_outbound_proxy_addr(outbound_proxy_addr)
    outbound_proxy_url = (
        f"http://{outbound_host}" if outbound_host else None
    )
    saved_proxy_env = setup_outbound_proxy_for_turn(
        workdir=workdir,
        session_id=session_id,
        proxy_addr=outbound_proxy_addr,
        proxy_api_key=outbound_proxy_api_key,
    )

    with provider_env(model):
        queue: asyncio.Queue[dict[str, Any] | None] = asyncio.Queue()

        async def emit_aux(event: dict[str, Any]) -> None:
            queue.put_nowait(event)

        configure_web_fetch(
            WebFetchRuntime(
                workdir=workdir,
                aux_model=aux_model,
                environment=environment,
                emit_event=emit_aux if aux_model is not None else None,
                outbound_proxy_url=outbound_proxy_url,
                outbound_proxy_api_key=outbound_proxy_api_key,
                session_id=session_id,
            ),
        )
        configure_call_agent(
            CallAgentRuntime(
                session_id=session_id,
                tenant_id=tenant_id,
                workdir=workdir,
                parent_agent=agent,
                sub_agents=sub_agents or {},
                model=model,
                aux_model=aux_model,
                environment=environment,
                emit_event=emit_aux if on_event is not None else None,
                mcp_proxy_base=mcp_proxy_base,
                mcp_proxy_api_key=mcp_proxy_api_key,
                outbound_proxy_addr=outbound_proxy_addr,
                outbound_proxy_api_key=outbound_proxy_api_key,
            ),
        )
        await setup_mcp_runtime_for_turn(
            mcp_servers=mcp_servers_from_agent(agent),
            session_id=session_id,
            proxy_base=mcp_proxy_base,
            proxy_api_key=mcp_proxy_api_key,
        )
        try:
            if create_session is not None:
                result = await create_session(None)
            else:
                tool_cfg = session_tool_config_from_agent(agent)
                result = await _default_create_session(
                    workdir=workdir,
                    model=resolved_model,
                    system_prompt=compose_system_prompt(agent.resolved_system_prompt),
                    builtin_tools=tool_cfg.builtin_tools,
                    extension_paths=tool_cfg.extension_paths,
                )
            session = result.session

            buffer: list[dict[str, Any]] = []
            raw_cursor = 0
            seen_agent_text: set[str] = set()
            oma_events: list[dict[str, Any]] = []

            async def drain_events() -> None:
                while True:
                    item = await queue.get()
                    if item is None:
                        break
                    oma_events.append(item)
                    if on_event is not None:
                        await on_event(item)

            drainer = asyncio.create_task(drain_events())

            def listener(event: Any) -> None:
                nonlocal raw_cursor
                _collect_pi_event(buffer, event)
                delta = emit_oma_events(
                    buffer[raw_cursor:],
                    seen_agent_text=seen_agent_text,
                )
                raw_cursor = len(buffer)
                for ev in delta:
                    queue.put_nowait(ev)

            if hasattr(session, "subscribe"):
                session.subscribe(listener)
            elif hasattr(session, "on"):
                session.on("event", listener)

            await session.prompt(prompt)
            if hasattr(session, "wait_for_idle"):
                await session.wait_for_idle()

            if not oma_events:
                fallback = emit_oma_events(
                    buffer,
                    seen_agent_text=seen_agent_text,
                )
                if not fallback:
                    text = _assistant_text_from_session(session)
                    if text:
                        fallback = [
                            {
                                "type": "agent.message",
                                "content": [{"type": "text", "text": text}],
                            }
                        ]
                for ev in fallback:
                    queue.put_nowait(ev)

            queue.put_nowait(None)
            await drainer

            if not oma_events:
                msg = "harness turn produced no assistant output"
                raise RuntimeError(msg)

            return TurnResponse(events=oma_events)
        finally:
            clear_outbound_proxy_for_turn(saved_proxy_env)
            clear_web_fetch_runtime()
            clear_mcp_runtime()
            clear_call_agent_runtime()


async def run_turn(
    *,
    session_id: str,
    tenant_id: str | None = None,
    agent: AgentSnapshot,
    sub_agents: dict[str, AgentSnapshot] | None = None,
    model: ModelConfig | None = None,
    aux_model: ModelConfig | None = None,
    environment: dict[str, Any] | None = None,
    events: list[dict[str, Any]],
    workdir: str,
    mcp_proxy_base: str | None = None,
    mcp_proxy_api_key: str | None = None,
    outbound_proxy_addr: str | None = None,
    outbound_proxy_api_key: str | None = None,
    create_session: CreateSessionFn | None = None,
) -> TurnResponse:
    return await _run_turn_core(
        session_id=session_id,
        tenant_id=tenant_id,
        agent=agent,
        sub_agents=sub_agents,
        model=model,
        aux_model=aux_model,
        environment=environment,
        events=events,
        workdir=workdir,
        mcp_proxy_base=mcp_proxy_base,
        mcp_proxy_api_key=mcp_proxy_api_key,
        outbound_proxy_addr=outbound_proxy_addr,
        outbound_proxy_api_key=outbound_proxy_api_key,
        create_session=create_session,
        on_event=None,
    )


async def run_turn_stream(
    *,
    session_id: str,
    tenant_id: str | None = None,
    agent: AgentSnapshot,
    sub_agents: dict[str, AgentSnapshot] | None = None,
    model: ModelConfig | None = None,
    aux_model: ModelConfig | None = None,
    environment: dict[str, Any] | None = None,
    events: list[dict[str, Any]],
    workdir: str,
    mcp_proxy_base: str | None = None,
    mcp_proxy_api_key: str | None = None,
    outbound_proxy_addr: str | None = None,
    outbound_proxy_api_key: str | None = None,
    create_session: CreateSessionFn | None = None,
    on_event: EventCallback,
) -> TurnResponse:
    return await _run_turn_core(
        session_id=session_id,
        tenant_id=tenant_id,
        agent=agent,
        sub_agents=sub_agents,
        model=model,
        aux_model=aux_model,
        environment=environment,
        events=events,
        workdir=workdir,
        mcp_proxy_base=mcp_proxy_base,
        mcp_proxy_api_key=mcp_proxy_api_key,
        outbound_proxy_addr=outbound_proxy_addr,
        outbound_proxy_api_key=outbound_proxy_api_key,
        create_session=create_session,
        on_event=on_event,
    )
