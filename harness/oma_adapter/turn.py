"""Stateless harness turn via piPy SDK."""

from __future__ import annotations

import asyncio
import os
from contextlib import contextmanager
from pathlib import Path
from typing import Any, Awaitable, Callable, Iterator

from oma_adapter.emit import emit_oma_events
from oma_adapter.platform_guidance import compose_system_prompt
from oma_adapter.project import project_oma_events
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


@contextmanager
def _provider_env(model: ModelConfig | None) -> Iterator[None]:
    if model is None or not model.api_key:
        yield
        return
    provider = (model.provider or "").lower()
    keys = ["ANTHROPIC_API_KEY"]
    if provider.startswith("oai") or provider == "openai":
        keys = ["OPENAI_API_KEY", "ANTHROPIC_API_KEY"]
    saved = {key: os.environ.get(key) for key in keys}
    try:
        os.environ[keys[0]] = model.api_key
        yield
    finally:
        for key, value in saved.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value


async def _run_turn_core(
    *,
    session_id: str,
    tenant_id: str | None = None,
    agent: AgentSnapshot,
    model: ModelConfig | None,
    aux_model: ModelConfig | None = None,
    environment: dict[str, Any] | None = None,
    events: list[dict[str, Any]],
    workdir: str,
    mcp_proxy_base: str | None = None,
    mcp_proxy_api_key: str | None = None,
    create_session: CreateSessionFn | None,
    on_event: EventCallback | None,
) -> TurnResponse:

    prompt = project_oma_events(events)
    if not prompt:
        return TurnResponse(events=[])

    resolved_model = model.model if model is not None else agent.model
    if not resolved_model.startswith("faux/") and os.environ.get("OMA_FAKE_HARNESS") == "1":
        resolved_model = "faux/test"

    with _provider_env(model):
        queue: asyncio.Queue[dict[str, Any] | None] = asyncio.Queue()

        async def emit_aux(event: dict[str, Any]) -> None:
            queue.put_nowait(event)

        configure_web_fetch(
            WebFetchRuntime(
                workdir=workdir,
                aux_model=aux_model,
                environment=environment,
                emit_event=emit_aux if aux_model is not None else None,
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
                    system_prompt=compose_system_prompt(agent.system_prompt),
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
            clear_web_fetch_runtime()
            clear_mcp_runtime()


async def run_turn(
    *,
    session_id: str,
    tenant_id: str | None = None,
    agent: AgentSnapshot,
    model: ModelConfig | None = None,
    aux_model: ModelConfig | None = None,
    environment: dict[str, Any] | None = None,
    events: list[dict[str, Any]],
    workdir: str,
    mcp_proxy_base: str | None = None,
    mcp_proxy_api_key: str | None = None,
    create_session: CreateSessionFn | None = None,
) -> TurnResponse:
    return await _run_turn_core(
        session_id=session_id,
        tenant_id=tenant_id,
        agent=agent,
        model=model,
        aux_model=aux_model,
        environment=environment,
        events=events,
        workdir=workdir,
        mcp_proxy_base=mcp_proxy_base,
        mcp_proxy_api_key=mcp_proxy_api_key,
        create_session=create_session,
        on_event=None,
    )


async def run_turn_stream(
    *,
    session_id: str,
    tenant_id: str | None = None,
    agent: AgentSnapshot,
    model: ModelConfig | None = None,
    aux_model: ModelConfig | None = None,
    environment: dict[str, Any] | None = None,
    events: list[dict[str, Any]],
    workdir: str,
    mcp_proxy_base: str | None = None,
    mcp_proxy_api_key: str | None = None,
    create_session: CreateSessionFn | None = None,
    on_event: EventCallback,
) -> TurnResponse:
    return await _run_turn_core(
        session_id=session_id,
        tenant_id=tenant_id,
        agent=agent,
        model=model,
        aux_model=aux_model,
        environment=environment,
        events=events,
        workdir=workdir,
        mcp_proxy_base=mcp_proxy_base,
        mcp_proxy_api_key=mcp_proxy_api_key,
        create_session=create_session,
        on_event=on_event,
    )
