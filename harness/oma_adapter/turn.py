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
from oma_adapter.subagent_bridge import build_subagent_runtime
from pi_subagent.runtime import (
    clear_subagent_runtime,
    configure_subagent_runtime,
    get_subagent_runtime,
)
from oma_adapter.emit import emit_oma_events
from oma_adapter.platform_guidance import compose_system_prompt
from oma_adapter.project import latest_user_text, project_oma_events
from oma_adapter.provider_env import provider_env
from oma_adapter.sandbox_paths import patch_path_utils
from oma_adapter.outbound.bash_ops import OutboundBashOperations
from oma_adapter.outbound.setup import (
    clear_outbound_proxy_for_turn,
    normalize_outbound_proxy_addr,
    setup_outbound_proxy_for_turn,
)
from oma_adapter.resource_mounter import mount_resources
from oma_adapter.mcp.runtime import McpRuntime, clear_mcp_runtime, configure_mcp_runtime
from oma_adapter.mcp.setup import (
    discover_mcp_tools,
    mcp_servers_from_agent,
)
from oma_adapter.tools import (
    enabled_schedule_tools,
    session_tool_config_from_agent,
)
from oma_adapter.pi_model import resolve_session_model_pattern
from oma_adapter.types import AgentSnapshot, ModelConfig, TurnResponse
from oma_adapter.web_fetch.runtime import WebFetchRuntime, clear_web_fetch_runtime, configure_web_fetch
from oma_adapter.web_search.runtime import (
    WebSearchRuntime,
    clear_web_search_runtime,
    configure_web_search,
    resolve_search_backend,
)
from oma_adapter.schedule.runtime import (
    ScheduleRuntime,
    clear_schedule_runtime,
    configure_schedule,
)
from oma_adapter.text_tools import (
    execute_prompted_mcp_tools,
    execute_text_tool_calls,
    mcp_tools_named_in_text,
)

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


def _should_use_fake_harness(
    *,
    model: ModelConfig | None,
    wire_model: str,
) -> bool:
    """Use faux/test only when fake mode is on and no real credentials arrived."""
    if os.environ.get("OMA_FAKE_HARNESS") != "1":
        return False
    if wire_model.startswith("faux/"):
        return False
    if model is not None and model.api_key:
        return False
    return True


def _register_mcp_tools_on_session(
    session: Any,
    mcp_runtime: McpRuntime,
) -> None:
    """Attach MCP tools after session creation (avoids extension load races)."""
    if not mcp_runtime.tools:
        return
    existing = {getattr(tool, "name", "") for tool in session._agent._tools}
    new_tools = [
        tool
        for tool in mcp_runtime.tools
        if getattr(tool, "name", "") not in existing
    ]
    if not new_tools:
        return
    session._agent._tools = [*session._agent._tools, *new_tools]
    resources = getattr(session, "_resources", None)
    if resources is not None and hasattr(resources, "extension_runtime"):
        resources.extension_runtime.tools.extend(new_tools)


def _events_include_tool_result(
    events: list[dict[str, Any]],
    tool_names: set[str],
) -> bool:
    """True when any listed tool already has a tool_result event."""
    if not tool_names:
        return False
    saw_use: set[str] = set()
    for item in events:
        kind = item.get("type")
        if kind == "agent.tool_use":
            name = item.get("name")
            if isinstance(name, str) and name in tool_names:
                saw_use.add(name)
        if kind != "agent.tool_result":
            continue
        for block in item.get("content") or []:
            text = (block.get("text") or "").strip()
            if not text or text.startswith("Error:"):
                continue
            if saw_use.intersection(tool_names):
                return True
    return False


def _resolve_turn_workdir(workdir: str) -> str:
    """Normalize sandbox path so harness and platform agree on .curlrc location."""
    resolved = Path(workdir).expanduser().resolve()
    return str(resolved)


async def _default_create_session(
    *,
    workdir: str,
    model: str,
    pi_provider: str | None = None,
    api_key: str | None = None,
    system_prompt: str | None,
    builtin_tools: list[str],
    extension_paths: list[str],
    outbound_curl_home: str | None = None,
) -> Any:
    from pi_coding_agent.sdk import CreateAgentSessionOptions, create_agent_session

    opts = CreateAgentSessionOptions(
        cwd=Path(workdir),
        model=model,
        provider=pi_provider,
        api_key=api_key,
        system_prompt=system_prompt,
        tools=builtin_tools,
        extension_paths=extension_paths or None,
        in_memory=True,
    )
    result = await create_agent_session(opts)
    if outbound_curl_home:
        _wire_outbound_bash_proxy(result.session, outbound_curl_home)
    return result


def _wire_outbound_bash_proxy(session: Any, workdir: str) -> None:
    """Ensure bash subprocesses read sandbox .curlrc via CURL_HOME."""
    curl_home = _resolve_turn_workdir(workdir)
    curlrc = Path(curl_home) / ".curlrc"
    if not curlrc.is_file():
        return
    agent = getattr(session, "_agent", None)
    if agent is None and hasattr(session, "agent"):
        agent = session.agent
    if agent is None:
        return
    tools = getattr(agent, "_tools", None)
    if not tools:
        return
    ops = OutboundBashOperations(curl_home=curl_home)
    for tool in tools:
        if getattr(tool, "name", None) != "bash":
            continue
        tool.operations = ops


async def _run_turn_core(
    *,
    session_id: str,
    tenant_id: str | None = None,
    agent: AgentSnapshot,
    sub_agents: dict[str, AgentSnapshot] | None = None,
    model: ModelConfig | None,
    aux_model: ModelConfig | None = None,
    environment: dict[str, Any] | None = None,
    resources: list[dict[str, Any]] | None = None,
    events: list[dict[str, Any]],
    workdir: str,
    mcp_proxy_base: str | None = None,
    mcp_proxy_api_key: str | None = None,
    outbound_proxy_addr: str | None = None,
    outbound_proxy_api_key: str | None = None,
    platform_base: str | None = None,
    internal_secret: str | None = None,
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

    wire_model = model.model if model is not None else agent.model
    if _should_use_fake_harness(model=model, wire_model=wire_model):
        wire_model = "faux/test"
    session_model, pi_provider = resolve_session_model_pattern(
        wire_model=wire_model,
        oma_provider=model.provider if model is not None else None,
    )

    workdir = _resolve_turn_workdir(workdir)
    patch_path_utils(workdir)
    saved_env = mount_resources(workdir, resources)

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
    outbound_curl_home = saved_proxy_env.get("CURL_HOME")

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
        configure_web_search(
            WebSearchRuntime(
                environment=environment,
                outbound_proxy_url=outbound_proxy_url,
                outbound_proxy_api_key=outbound_proxy_api_key,
                session_id=session_id,
                backend=resolve_search_backend(agent),
                tavily_api_key=os.environ.get("TAVILY_API_KEY"),
            ),
        )
        schedule_enabled = enabled_schedule_tools(agent)
        if schedule_enabled:
            configure_schedule(
                ScheduleRuntime(
                    session_id=session_id,
                    platform_base=platform_base,
                    internal_secret=internal_secret or os.environ.get(
                        "OMA_INTERNAL_SECRET"
                    ),
                    enabled_tools=schedule_enabled,
                ),
            )
        configure_subagent_runtime(
            build_subagent_runtime(
                session_id=session_id,
                tenant_id=tenant_id,
                workdir=workdir,
                parent_agent=agent,
                sub_agents=sub_agents,
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
        mcp_servers = mcp_servers_from_agent(agent)
        mcp_runtime = await discover_mcp_tools(
            mcp_servers=mcp_servers,
            session_id=session_id,
            proxy_base=mcp_proxy_base,
            proxy_api_key=mcp_proxy_api_key,
        )
        configure_mcp_runtime(mcp_runtime if mcp_runtime.tools else None)
        try:
            if create_session is not None:
                result = await create_session(None)
            else:
                tool_cfg = session_tool_config_from_agent(agent)
                result = await _default_create_session(
                    workdir=workdir,
                    model=session_model,
                    pi_provider=pi_provider,
                    api_key=model.api_key if model is not None else None,
                    system_prompt=compose_system_prompt(agent.resolved_system_prompt),
                    builtin_tools=tool_cfg.builtin_tools,
                    extension_paths=tool_cfg.extension_paths,
                    outbound_curl_home=outbound_curl_home,
                )
            session = result.session
            _register_mcp_tools_on_session(session, mcp_runtime)

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

            _wire_outbound_bash_proxy(session, workdir)

            await session.prompt(prompt)
            if hasattr(session, "wait_for_idle"):
                await session.wait_for_idle()

            assistant_text = _assistant_text_from_session(session)
            user_text = latest_user_text(working_events)
            requested_mcp = set(mcp_tools_named_in_text(user_text))
            streamed_oma = emit_oma_events(
                buffer,
                seen_agent_text=seen_agent_text,
            )
            fallback_tool_events: list[dict[str, Any]] = []
            pending_mcp = requested_mcp & {
                getattr(tool, "name", "")
                for tool in mcp_runtime.tools
                if getattr(tool, "name", "")
            }
            need_mcp_fallback = bool(pending_mcp) and not _events_include_tool_result(
                streamed_oma,
                pending_mcp,
            )

            if assistant_text and "$$" in assistant_text:
                fallback_tool_events = await execute_text_tool_calls(
                    session,
                    assistant_text=assistant_text,
                    fallback_tools=mcp_runtime.tools,
                )
                for ev in fallback_tool_events:
                    queue.put_nowait(ev)

            if need_mcp_fallback and not _events_include_tool_result(
                fallback_tool_events,
                pending_mcp,
            ):
                for ev in await execute_prompted_mcp_tools(
                    session,
                    prompt_text=user_text or prompt,
                    mcp_runtime=mcp_runtime,
                ):
                    queue.put_nowait(ev)

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

            subagent_runtime = get_subagent_runtime()
            if subagent_runtime is not None and subagent_runtime.background_tasks:
                await asyncio.gather(
                    *subagent_runtime.background_tasks,
                    return_exceptions=True,
                )

            return TurnResponse(events=oma_events)
        finally:
            for key in saved_env:
                os.environ.pop(key, None)
            clear_outbound_proxy_for_turn(saved_proxy_env)
            clear_web_fetch_runtime()
            clear_web_search_runtime()
            clear_schedule_runtime()
            clear_mcp_runtime()
            clear_subagent_runtime()


async def run_turn(
    *,
    session_id: str,
    tenant_id: str | None = None,
    agent: AgentSnapshot,
    sub_agents: dict[str, AgentSnapshot] | None = None,
    model: ModelConfig | None = None,
    aux_model: ModelConfig | None = None,
    environment: dict[str, Any] | None = None,
    resources: list[dict[str, Any]] | None = None,
    events: list[dict[str, Any]],
    workdir: str,
    mcp_proxy_base: str | None = None,
    mcp_proxy_api_key: str | None = None,
    outbound_proxy_addr: str | None = None,
    outbound_proxy_api_key: str | None = None,
    platform_base: str | None = None,
    internal_secret: str | None = None,
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
        resources=resources,
        events=events,
        workdir=workdir,
        mcp_proxy_base=mcp_proxy_base,
        mcp_proxy_api_key=mcp_proxy_api_key,
        outbound_proxy_addr=outbound_proxy_addr,
        outbound_proxy_api_key=outbound_proxy_api_key,
        platform_base=platform_base,
        internal_secret=internal_secret,
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
    resources: list[dict[str, Any]] | None = None,
    events: list[dict[str, Any]],
    workdir: str,
    mcp_proxy_base: str | None = None,
    mcp_proxy_api_key: str | None = None,
    outbound_proxy_addr: str | None = None,
    outbound_proxy_api_key: str | None = None,
    platform_base: str | None = None,
    internal_secret: str | None = None,
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
        resources=resources,
        events=events,
        workdir=workdir,
        mcp_proxy_base=mcp_proxy_base,
        mcp_proxy_api_key=mcp_proxy_api_key,
        outbound_proxy_addr=outbound_proxy_addr,
        outbound_proxy_api_key=outbound_proxy_api_key,
        platform_base=platform_base,
        internal_secret=internal_secret,
        create_session=create_session,
        on_event=on_event,
    )
