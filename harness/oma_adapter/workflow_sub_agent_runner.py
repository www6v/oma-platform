"""OMA harness subAgent backend via pi_subagent run_sub_turn delegation.

When piPy-dynamic-workflows runs inside oma-platform harness, each workflow
agent() step delegates through the host ``SubAgentRuntime`` (same sandbox,
Console thread visibility).

The harness wires this runner in at startup via
``oma_adapter.workflow_integration.configure_workflow_oma_integration()``.
Without that wiring, workflow agent() steps fail fast via the workflow
package's ``TodoSubAgentRunner`` default.
"""

from __future__ import annotations

import logging
import re
import uuid
from typing import Any, Optional

from pi_dynamic_workflows.lib.model_tier import (
    harness_default_model,
    normalize_model_spec,
    resolve_model,
)
from pi_dynamic_workflows.lib.structured_output import (
    REPAIR_PROMPT,
    StructuredOutputError,
    extract_validated,
)
from pi_dynamic_workflows.lib.sub_agent_constants import TOOL_ALLOWLIST
from pi_dynamic_workflows.lib.sub_agent_types import (
    SubAgentOptions,
    SubAgentResult,
)
from oma_adapter.assistant_text import assemble_assistant_text
from oma_adapter.workflow_bootstrap import (
    WORKFLOW_WORKER_SYSTEM,
    get_workflow_worker_map,
)

logger = logging.getLogger(__name__)


def _pi_subagent_tool_configs(action: str) -> list[dict[str, Any]]:
    """Map workflow action allowlist to pi_subagent agent_toolset configs."""
    names = TOOL_ALLOWLIST.get(action, [])
    if not names:
        return [{"type": "agent_toolset_20260401"}]
    configs = [{"name": name, "enabled": True} for name in names]
    return [{"type": "agent_toolset_20260401", "configs": configs}]


def _resolve_model_id(opts: SubAgentOptions) -> str:
    spec = resolve_model({"model": opts.model, "tier": opts.tier})
    if spec:
        return normalize_model_spec(spec)
    runtime_model = _parent_model_from_runtime()
    if runtime_model:
        return runtime_model
    return normalize_model_spec(harness_default_model())


def _parent_model_from_runtime() -> str | None:
    try:
        from pi_subagent.runtime import get_subagent_runtime
    except ImportError:
        return None
    runtime = get_subagent_runtime()
    if runtime is None:
        return None
    parent = runtime.parent_agent
    return parent.model or parent.aux_model


async def _emit_runtime_event(runtime: Any, event: dict[str, Any]) -> None:
    if runtime.emit_event is not None:
        await runtime.emit_event(event)


async def _emit_sub_thread_created(
    runtime: Any,
    *,
    thread_id: str,
    agent_id: str,
    agent_name: str,
) -> None:
    """Mirror pi_subagent.delegate lifecycle so Console lists sub threads."""
    parent_thread = getattr(runtime, "parent_thread_id", None) or "sthr_primary"
    await _emit_runtime_event(
        runtime,
        {
            "type": "session.thread_created",
            "session_thread_id": thread_id,
            "agent_id": agent_id,
            "agent_name": agent_name,
            "parent_thread_id": parent_thread,
        },
    )


async def _emit_sub_thread_idle(runtime: Any, *, thread_id: str) -> None:
    await _emit_runtime_event(
        runtime,
        {
            "type": "session.thread_idle",
            "session_thread_id": thread_id,
        },
    )


def _sanitize_agent_id(agent_id: str) -> str:
    return re.sub(r"[^a-zA-Z0-9_]", "_", agent_id)


async def _emit_primary_delegation_start(
    runtime: Any,
    *,
    tool_id: str,
    agent_id: str,
    step_label: str,
    prompt: str,
    thread_id: str,
) -> None:
    """Mirror call_agent tool_use on the coordinator (Main) thread."""
    await _emit_runtime_event(
        runtime,
        {
            "type": "agent.tool_use",
            "session_thread_id": "sthr_primary",
            "id": tool_id,
            "name": f"call_agent_{_sanitize_agent_id(agent_id)}",
            "input": {
                "message": prompt,
                "step": step_label,
                "thread_id": thread_id,
            },
        },
    )


async def _emit_primary_delegation_end(
    runtime: Any,
    *,
    tool_id: str,
    result_text: str,
    is_error: bool = False,
) -> None:
    """Mirror call_agent tool_result on the coordinator (Main) thread."""
    if result_text:
        body = result_text
    elif is_error:
        body = "Sub-agent failed"
    else:
        body = "Sub-agent completed with no text output"
    await _emit_runtime_event(
        runtime,
        {
            "type": "agent.tool_result",
            "session_thread_id": "sthr_primary",
            "tool_use_id": tool_id,
            "content": [{"type": "text", "text": body[:8000]}],
            **({"is_error": True} if is_error else {}),
        },
    )


def _build_worker_snapshot(
    opts: SubAgentOptions,
    model_id: str,
    runtime: Any,
) -> Any:
    from pi_subagent.types import SubAgentSnapshot

    label = opts.label or "workflow-agent"
    worker_map = get_workflow_worker_map()
    agent_id = worker_map.get(label) if worker_map else None
    if agent_id and agent_id in runtime.sub_agents:
        snap = runtime.sub_agents[agent_id]
        return snap.model_copy(
            update={
                "tools": _pi_subagent_tool_configs(opts.action),
            },
        )

    return SubAgentSnapshot(
        id=f"wf-{label}",
        name=label,
        model=model_id,
        system_prompt=WORKFLOW_WORKER_SYSTEM,
        tools=_pi_subagent_tool_configs(opts.action),
        version=1,
    )


class OmaSubAgentRunner:
    """Delegates workflow steps to OMA harness sub-turn execution."""

    async def run(
        self,
        prompt: str,
        options: Optional[SubAgentOptions] = None,
    ) -> SubAgentResult:
        opts = options or SubAgentOptions()
        try:
            from pi_subagent.events import new_thread_id
            from pi_subagent.runtime import get_subagent_runtime
        except ImportError as exc:
            raise RuntimeError(
                "pi_subagent is required for workflow sub-agent execution"
            ) from exc

        runtime = get_subagent_runtime()
        if runtime is None:
            raise RuntimeError(
                "No SubAgentRuntime context; OMA bootstrap must run before "
                "workflow execution (see OmaWorkflowBootstrap.setup)"
            )

        model_id = _resolve_model_id(opts)
        worker = _build_worker_snapshot(opts, model_id, runtime)
        thread_id = new_thread_id()

        logger.info(
            "subAgent '%s' action=%s model=%s backend=oma thread=%s",
            opts.label or "agent",
            opts.action,
            model_id,
            thread_id,
        )

        worker_map = get_workflow_worker_map()
        agent_id = (
            worker_map.get(opts.label or "")
            if worker_map
            else None
        ) or worker.id

        step_label = opts.label or "workflow-agent"
        task_id = f"task_{thread_id[-12:]}"
        tool_id = f"wf_{uuid.uuid4().hex[:12]}"

        await _emit_sub_thread_created(
            runtime,
            thread_id=thread_id,
            agent_id=agent_id,
            agent_name=worker.name,
        )
        await _emit_runtime_event(
            runtime,
            {
                "type": "session.sub_agent_started",
                "task_id": task_id,
                "session_thread_id": thread_id,
                "agent_id": agent_id,
                "agent_name": worker.name,
            },
        )
        await _emit_primary_delegation_start(
            runtime,
            tool_id=tool_id,
            agent_id=agent_id,
            step_label=step_label,
            prompt=prompt,
            thread_id=thread_id,
        )
        await _emit_runtime_event(
            runtime,
            {
                "type": "user.message",
                "session_thread_id": thread_id,
                "content": [{"type": "text", "text": prompt}],
            },
        )

        text = ""
        structured = None
        step_error: BaseException | None = None
        try:
            turn_result = await runtime.run_sub_turn(
                session_id=runtime.session_id,
                tenant_id=runtime.tenant_id,
                agent=worker,
                message=prompt,
                workdir=opts.cwd or runtime.workdir,
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

            # OMA emit streams deltas as many agent.message events; join them.
            text = assemble_assistant_text(turn_result.events)
            structured = None
            if opts.output_schema:
                structured = extract_validated(text, opts.output_schema)
                retries = max(0, opts.max_schema_retries)
                attempt = 0
                while structured is None and attempt < retries:
                    attempt += 1
                    repair = await runtime.run_sub_turn(
                        session_id=runtime.session_id,
                        tenant_id=runtime.tenant_id,
                        agent=worker,
                        message=REPAIR_PROMPT,
                        workdir=opts.cwd or runtime.workdir,
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
                    text = assemble_assistant_text(repair.events)
                    structured = extract_validated(text, opts.output_schema)

                if structured is None:
                    raise StructuredOutputError(
                        f"SubAgent '{opts.label}' did not produce valid structured "
                        "output after repair attempts",
                        step_name=opts.label,
                    )
        except BaseException as exc:
            step_error = exc
            raise
        finally:
            summary = text[:500] if text else ""
            if step_error is not None and not summary:
                summary = str(step_error)
            await _emit_primary_delegation_end(
                runtime,
                tool_id=tool_id,
                result_text=summary,
                is_error=step_error is not None,
            )
            await _emit_runtime_event(
                runtime,
                {
                    "type": "session.sub_agent_completed",
                    "task_id": task_id,
                    "session_thread_id": thread_id,
                    "agent_id": agent_id,
                    "summary": summary or (
                        "Sub-agent completed with no text output"
                    ),
                    **({"is_error": True} if step_error is not None else {}),
                },
            )
            await _emit_sub_thread_idle(runtime, thread_id=thread_id)

        if not text and structured is None:
            return SubAgentResult(
                content="",
                model=model_id,
                source="oma_sub_agent",
                warning="no assistant text in sub-turn events",
            )

        return SubAgentResult(
            content=text,
            model=model_id,
            source="oma_sub_agent",
            structured=structured,
        )
