"""Stateless harness turn via piPy SDK."""

from __future__ import annotations

import asyncio
import logging
import os
from pathlib import Path
from typing import Any, Awaitable, Callable

from oma_adapter.compaction import (
    compact_events,
    resolve_context_window_tokens,
    should_compact,
)
from oma_adapter.env_packages import ensure_environment_packages
from oma_adapter.subagent_bridge import build_subagent_runtime
from oma_adapter.team_bridge import build_team_runtime
from pi_subagent.resolve import enrich_parent_for_delegation, resolve_sub_agents
from pi_subagent.types import SubAgentSnapshot
from pi_subagent.runtime import (
    clear_subagent_runtime,
    configure_subagent_runtime,
    get_subagent_runtime,
)
from oma_adapter.custom_tools import (
    custom_tool_names,
    custom_tools_from_agent,
    pending_custom_tool_ids,
    register_custom_tools_on_session,
)
from oma_adapter.custom_tools_runtime import (
    CustomToolsRuntime,
    clear_custom_tools_runtime,
    configure_custom_tools_runtime,
)
from oma_adapter.emit import emit_oma_events
from oma_adapter.platform_guidance import (
    compose_system_prompt,
    memory_platform_reminders,
)
from oma_adapter.skill_mounter import mount_skills, skill_platform_reminders
from oma_adapter.project import (
    PRIMARY_THREAD_ID,
    filter_events_for_thread,
    latest_user_text,
    project_oma_events,
)
from oma_adapter.provider_env import provider_env
from oma_adapter.sandbox_paths import is_remote_sandbox_provider, patch_path_utils
from oma_adapter.outbound.bash_ops import OutboundBashOperations
from oma_adapter.outbound.platform_exec_bash import PlatformExecBashOperations
from oma_adapter.outbound.setup import (
    clear_outbound_proxy_for_turn,
    normalize_outbound_proxy_addr,
    setup_outbound_proxy_for_turn,
)
from oma_adapter.resource_mounter import ensure_session_output_mounts, mount_resources
from oma_adapter.mcp.runtime import McpRuntime, clear_mcp_runtime, configure_mcp_runtime
from oma_adapter.mcp.setup import (
    discover_mcp_tools,
    mcp_servers_from_agent,
)
from oma_adapter.tools import (
    enabled_schedule_tools,
    session_tool_config_from_agent,
)
from oma_adapter.pi_model import (
    is_legacy_anthropic_model,
    normalize_harness_models,
    resolve_session_model_pattern,
)
from oma_adapter.types import AgentSnapshot, ModelConfig, TurnResponse
from oma_adapter.web_fetch.runtime import WebFetchRuntime, clear_web_fetch_runtime, configure_web_fetch
from oma_adapter.web_search.runtime import (
    WebSearchRuntime,
    clear_web_search_runtime,
    configure_web_search,
    resolve_search_backend,
)
from pi_team.runtime import configure_team_runtime, get_team_runtime, reset_team_runtime
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
        # Skip non-dict items (defensive)
        if not isinstance(item, dict):
            continue
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



def _thinking_level_from_agent(agent: AgentSnapshot | None) -> str | None:
    """Read thinking level from agent.metadata (off|minimal|low|medium|high|xhigh)."""
    if agent is None or not agent.metadata:
        return None
    level = agent.metadata.get("thinking_level")
    if isinstance(level, str) and level.strip():
        return level.strip()
    return None


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
    session_id: str | None = None,
    sandbox_provider: str | None = None,
    platform_base: str | None = None,
    thinking_level: str | None = None,
) -> Any:
    from pi_coding_agent.sdk import CreateAgentSessionOptions, create_agent_session

    opts = CreateAgentSessionOptions(
        cwd=Path(workdir),
        model=model,
        provider=pi_provider,
        api_key=None,  # let piPy resolve from ~/.pi/agent/auth.json; platform key may be stale
        system_prompt=system_prompt,
        tools=builtin_tools,
        extension_paths=extension_paths or None,
        in_memory=True,
        thinking_level=thinking_level,
    )
    result = await create_agent_session(opts)
    _wire_sandbox_bash_proxy(
        result.session,
        workdir=outbound_curl_home or workdir,
        session_id=session_id or "",
        sandbox_provider=sandbox_provider,
        platform_base=platform_base,
    )
    return result


def _wire_sandbox_bash_proxy(
    session: Any,
    *,
    workdir: str,
    session_id: str,
    sandbox_provider: str | None,
    platform_base: str | None,
) -> None:
    """Route bash to platform exec API (remote) or local subprocess."""
    agent = getattr(session, "_agent", None)
    if agent is None and hasattr(session, "agent"):
        agent = session.agent
    if agent is None:
        return
    tools = getattr(agent, "_tools", None)
    if not tools:
        return
    if is_remote_sandbox_provider(sandbox_provider) and platform_base and session_id:
        ops = PlatformExecBashOperations(
            session_id=session_id,
            platform_base=platform_base,
            workdir="/workspace",
        )
    else:
        curl_home = _resolve_turn_workdir(workdir)
        curlrc = Path(curl_home) / ".curlrc"
        if not curlrc.is_file():
            return
        ops = OutboundBashOperations(curl_home=curl_home)
    for tool in tools:
        if getattr(tool, "name", None) != "bash":
            continue
        tool.operations = ops


async def _run_turn_core(
    *,
    session_id: str,
    session_thread_id: str | None = None,
    tenant_id: str | None = None,
    agent: AgentSnapshot,
    sub_agents: dict[str, AgentSnapshot] | None = None,
    model: ModelConfig | None,
    aux_model: ModelConfig | None = None,
    environment: dict[str, Any] | None = None,
    resources: list[dict[str, Any]] | None = None,
    skills: list[dict[str, Any]] | None = None,
    events: list[dict[str, Any]],
    workdir: str,
    mcp_proxy_base: str | None = None,
    mcp_proxy_api_key: str | None = None,
    outbound_proxy_addr: str | None = None,
    outbound_proxy_api_key: str | None = None,
    platform_base: str | None = None,
    internal_secret: str | None = None,
    database_path: str | None = None,
    sandbox_provider: str | None = None,
    create_session: CreateSessionFn | None,
    on_event: EventCallback | None,
) -> TurnResponse:

    working_events = filter_events_for_thread(
        list(events),
        session_thread_id,
    )
    thread_tag = session_thread_id or PRIMARY_THREAD_ID
    is_subthread = thread_tag != PRIMARY_THREAD_ID

    delegation_agent = SubAgentSnapshot.model_validate(agent.model_dump())
    agent_custom_tools = custom_tool_names(
        AgentSnapshot.model_validate(delegation_agent.model_dump())
    )
    resolved_sub_agents: dict[str, SubAgentSnapshot] = {}
    if sub_agents:
        resolved_sub_agents = {
            aid: SubAgentSnapshot.model_validate(s.model_dump())
            for aid, s in sub_agents.items()
        }
    if not is_subthread:
        delegation_agent = enrich_parent_for_delegation(delegation_agent)
        try:
            resolved_sub_agents = resolve_sub_agents(
                parent=delegation_agent,
                tenant_id=tenant_id,
                database_path=database_path,
            )
        except ValueError as exc:
            raise RuntimeError(str(exc)) from exc

    async def tagged_on_event(event: dict[str, Any]) -> None:
        if is_subthread:
            tagged = dict(event)
            tagged["session_thread_id"] = thread_tag
            event = tagged
        if on_event is not None:
            await on_event(event)

    model, aux_model = normalize_harness_models(
        model,
        agent_model=agent.model,
        aux_model=aux_model,
    )
    if model is not None and is_legacy_anthropic_model(agent.model):
        logging.getLogger("oma.turn").warning(
            "[turn] remapped legacy model %s -> %s (provider=%s)",
            agent.model,
            model.model,
            model.provider,
        )

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
                await tagged_on_event(boundary)

    prompt = project_oma_events(
        working_events,
        session_thread_id=session_thread_id,
    )
    if not prompt:
        return TurnResponse(events=[], pending_custom_tool_ids=[])

    wire_model = model.model if model is not None else agent.model
    if _should_use_fake_harness(model=model, wire_model=wire_model):
        wire_model = "faux/test"
    session_model, pi_provider = resolve_session_model_pattern(
        wire_model=wire_model,
        oma_provider=model.provider if model is not None else None,
    )

    workdir = _resolve_turn_workdir(workdir)
    patch_path_utils(workdir)
    ensure_session_output_mounts(workdir)
    saved_env = mount_resources(workdir, resources)
    mount_skills(workdir, skills)
    ensure_environment_packages(environment)

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
        import logging as _logging
        _logger = _logging.getLogger("oma.turn")
        team_runtime = None
        team_runtime_token = None
        if not is_subthread:
            _logger.warning(
                "[turn] build_team_runtime: session=%s enable_team_tools=%s metadata=%s",
                session_id,
                agent.enable_team_tools,
                agent.metadata,
            )
            team_runtime = build_team_runtime(
                session_id=session_id,
                tenant_id=tenant_id,
                platform_base=platform_base,
                internal_secret=internal_secret or os.environ.get(
                    "OMA_INTERNAL_SECRET"
                ),
                agent=agent,
                database_path=database_path,
            )
            _logger.warning(
                "[turn] build_team_runtime result: %s",
                type(team_runtime).__name__ if team_runtime is not None else "None",
            )
        if team_runtime is not None:
            team_runtime_token = configure_team_runtime(team_runtime)
            from pi_team.shutdown import drain_pending_shutdowns

            try:
                asyncio.get_running_loop().create_task(
                    drain_pending_shutdowns(team_runtime.session_id or "")
                )
            except RuntimeError:
                pass
        configure_subagent_runtime(
            build_subagent_runtime(
                session_id=session_id,
                tenant_id=tenant_id,
                workdir=workdir,
                parent_agent=AgentSnapshot.model_validate(
                    delegation_agent.model_dump()
                ),
                sub_agents={
                    aid: AgentSnapshot.model_validate(s.model_dump())
                    for aid, s in resolved_sub_agents.items()
                },
                model=model,
                aux_model=aux_model,
                environment=environment,
                emit_event=tagged_on_event if on_event is not None else None,
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
        agent_for_tools = AgentSnapshot.model_validate(delegation_agent.model_dump())
        custom_tool_defs = custom_tools_from_agent(agent_for_tools)
        configure_custom_tools_runtime(
            CustomToolsRuntime(tools=custom_tool_defs) if custom_tool_defs else None
        )
        try:
            if create_session is not None:
                result = await create_session(None)
            else:
                tool_cfg = session_tool_config_from_agent(
                    AgentSnapshot.model_validate(delegation_agent.model_dump())
                )
                result = await _default_create_session(
                    workdir=workdir,
                    model=session_model,
                    pi_provider=pi_provider,
                    api_key=model.api_key if model is not None else None,
                    system_prompt=compose_system_prompt(
                        agent.resolved_system_prompt,
                        memory_platform_reminders(resources)
                        + skill_platform_reminders(skills),
                    ),
                    builtin_tools=tool_cfg.builtin_tools,
                    extension_paths=tool_cfg.extension_paths,
                    outbound_curl_home=outbound_curl_home,
                    session_id=session_id,
                    sandbox_provider=sandbox_provider,
                    platform_base=platform_base,
                    thinking_level=_thinking_level_from_agent(agent),
                )
            session = result.session
            _register_mcp_tools_on_session(session, mcp_runtime)
            register_custom_tools_on_session(session, custom_tool_defs)

            buffer: list[dict[str, Any]] = []
            raw_cursor = 0
            seen_agent_text: set[str] = set()
            streaming_state: dict[str, Any] = {"last_emitted_len": 0}
            oma_events: list[dict[str, Any]] = []

            import logging as _tlog
            _tl = _tlog.getLogger("oma.turn")
            _tl.warning("[turn_exec] session type=%s has_subscribe=%s has_on=%s session_id=%s",
                type(session).__name__, hasattr(session, "subscribe"), hasattr(session, "on"), session_id)

            async def drain_events() -> None:
                while True:
                    item = await queue.get()
                    if item is None:
                        break
                    oma_events.append(item)
                    if on_event is not None:
                        await tagged_on_event(item)

            drainer = asyncio.create_task(drain_events())

            def listener(event: Any) -> None:
                nonlocal raw_cursor
                _collect_pi_event(buffer, event)
                delta = emit_oma_events(
                    buffer[raw_cursor:],
                    seen_agent_text=seen_agent_text,
                    custom_tool_names=agent_custom_tools,
                    event_lookup_buffer=buffer,
                    streaming_state=streaming_state,
                )
                raw_cursor = len(buffer)
                # Debug: log what we got and what we emitted
                import sys
                print(f"DEBUG listener: buffer_len={len(buffer)}, delta_len={len(delta)}, last_event_type={buffer[-1].get('type') if buffer and isinstance(buffer[-1], dict) else 'N/A'}", file=sys.stderr, flush=True)
                for ev in delta:
                    queue.put_nowait(ev)

            if hasattr(session, "subscribe"):
                session.subscribe(listener)
            elif hasattr(session, "on"):
                session.on("event", listener)

            _wire_sandbox_bash_proxy(
                session,
                workdir=workdir,
                session_id=session_id,
                sandbox_provider=sandbox_provider,
                platform_base=platform_base,
            )

            _tl.warning("[turn_exec] calling session.prompt session_id=%s prompt_len=%d", session_id, len(prompt))
            await session.prompt(prompt)
            _tl.warning("[turn_exec] prompt() done, buffer=%d session_id=%s", len(buffer), session_id)
            if hasattr(session, "wait_for_idle"):
                await session.wait_for_idle()
            _tl.warning("[turn_exec] idle, buffer=%d oma_events=%d session_id=%s", len(buffer), len(oma_events), session_id)
            if not oma_events and buffer:
                import json as _json
                # Defensive: filter out non-dict items before calling .get()
                event_types = [
                    (e.get("type") or e.get("event", "?")) if isinstance(e, dict) else f"<{type(e).__name__}>"
                    for e in buffer
                ]
                _tl.warning("[turn_exec] buffer event types: %s", event_types[:30])
                # log first and last event content for diagnosis
                for idx in range(min(len(buffer), 30)):
                    _tl.warning("[turn_exec] buffer[%d]: %s", idx, _json.dumps(buffer[idx], default=str)[:800])

            assistant_text = _assistant_text_from_session(session)
            user_text = latest_user_text(
                working_events,
                session_thread_id=session_thread_id,
            )
            requested_mcp = set(mcp_tools_named_in_text(user_text))
            # Debug: log buffer types before emit_oma_events
            buffer_types = set(type(x).__name__ for x in buffer)
            _tl.warning("[turn_exec] buffer types before emit: %s", buffer_types)
            streamed_oma = emit_oma_events(
                buffer,
                seen_agent_text=seen_agent_text,
                custom_tool_names=agent_custom_tools,
                streaming_state=streaming_state,
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
                    custom_tool_names=agent_custom_tools,
                    streaming_state=streaming_state,
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

            pending_ids = pending_custom_tool_ids(oma_events)
            return TurnResponse(
                events=oma_events,
                pending_custom_tool_ids=pending_ids,
            )
        finally:
            for key in saved_env:
                os.environ.pop(key, None)
            clear_outbound_proxy_for_turn(saved_proxy_env)
            clear_web_fetch_runtime()
            clear_web_search_runtime()
            clear_schedule_runtime()
            clear_mcp_runtime()
            clear_custom_tools_runtime()
            clear_subagent_runtime()
            if team_runtime_token is not None:
                active_team = get_team_runtime()
                if active_team is not None and active_team.session_id:
                    try:
                        asyncio.get_running_loop().create_task(
                            active_team.loop_manager.stop_all_for_session(
                                active_team.session_id
                            )
                        )
                    except RuntimeError:
                        pass
                reset_team_runtime(team_runtime_token)


async def run_turn(
    *,
    session_id: str,
    session_thread_id: str | None = None,
    tenant_id: str | None = None,
    agent: AgentSnapshot,
    sub_agents: dict[str, AgentSnapshot] | None = None,
    model: ModelConfig | None = None,
    aux_model: ModelConfig | None = None,
    environment: dict[str, Any] | None = None,
    resources: list[dict[str, Any]] | None = None,
    skills: list[dict[str, Any]] | None = None,
    events: list[dict[str, Any]],
    workdir: str,
    mcp_proxy_base: str | None = None,
    mcp_proxy_api_key: str | None = None,
    outbound_proxy_addr: str | None = None,
    outbound_proxy_api_key: str | None = None,
    platform_base: str | None = None,
    internal_secret: str | None = None,
    database_path: str | None = None,
    sandbox_provider: str | None = None,
    create_session: CreateSessionFn | None = None,
) -> TurnResponse:
    return await _run_turn_core(
        session_id=session_id,
        session_thread_id=session_thread_id,
        tenant_id=tenant_id,
        agent=agent,
        sub_agents=sub_agents,
        model=model,
        aux_model=aux_model,
        environment=environment,
        resources=resources,
        skills=skills,
        events=events,
        workdir=workdir,
        mcp_proxy_base=mcp_proxy_base,
        mcp_proxy_api_key=mcp_proxy_api_key,
        outbound_proxy_addr=outbound_proxy_addr,
        outbound_proxy_api_key=outbound_proxy_api_key,
        platform_base=platform_base,
        internal_secret=internal_secret,
        database_path=database_path,
        sandbox_provider=sandbox_provider,
        create_session=create_session,
        on_event=None,
    )


async def run_turn_stream(
    *,
    session_id: str,
    session_thread_id: str | None = None,
    tenant_id: str | None = None,
    agent: AgentSnapshot,
    sub_agents: dict[str, AgentSnapshot] | None = None,
    model: ModelConfig | None = None,
    aux_model: ModelConfig | None = None,
    environment: dict[str, Any] | None = None,
    resources: list[dict[str, Any]] | None = None,
    skills: list[dict[str, Any]] | None = None,
    events: list[dict[str, Any]],
    workdir: str,
    mcp_proxy_base: str | None = None,
    mcp_proxy_api_key: str | None = None,
    outbound_proxy_addr: str | None = None,
    outbound_proxy_api_key: str | None = None,
    platform_base: str | None = None,
    internal_secret: str | None = None,
    database_path: str | None = None,
    sandbox_provider: str | None = None,
    create_session: CreateSessionFn | None = None,
    on_event: EventCallback,
) -> TurnResponse:
    return await _run_turn_core(
        session_id=session_id,
        session_thread_id=session_thread_id,
        tenant_id=tenant_id,
        agent=agent,
        sub_agents=sub_agents,
        model=model,
        aux_model=aux_model,
        environment=environment,
        resources=resources,
        skills=skills,
        events=events,
        workdir=workdir,
        mcp_proxy_base=mcp_proxy_base,
        mcp_proxy_api_key=mcp_proxy_api_key,
        outbound_proxy_addr=outbound_proxy_addr,
        outbound_proxy_api_key=outbound_proxy_api_key,
        platform_base=platform_base,
        internal_secret=internal_secret,
        database_path=database_path,
        sandbox_provider=sandbox_provider,
        create_session=create_session,
        on_event=on_event,
    )
