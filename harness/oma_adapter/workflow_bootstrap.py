"""OMA bootstrap for workflow executions.

Creates workers + coordinator agents and a session on the OMA platform via
``oma_sdk``, then configures ``SubAgentRuntime`` so workflow agent() steps
delegate through the harness sandbox.

Implements the ``WorkflowBootstrap`` Protocol from
``pi_dynamic_workflows.lib.workflow_bootstrap``. The harness wires
``OmaWorkflowBootstrap()`` in at startup via
``oma_adapter.workflow_integration.configure_workflow_oma_integration()``.
"""

from __future__ import annotations

import asyncio
import logging
import os
from contextvars import ContextVar, Token
from pathlib import Path
from typing import Any, Awaitable, Callable, Dict, List, Optional, Tuple
from urllib.parse import urljoin

import httpx

from pi_dynamic_workflows.lib.model_tier import (
    harness_default_model,
    normalize_model_spec,
    resolve_model,
)
from pi_dynamic_workflows.lib.sub_agent_constants import AGENT_ACTIONS, TOOL_ALLOWLIST
from pi_dynamic_workflows.lib.workflow_bootstrap import (
    WorkflowBootstrap,
    WorkflowBootstrapContext,
)

from oma_adapter.workflow_oma_sdk import is_oma_sdk_available

logger = logging.getLogger(__name__)


# ---------------------------------------------------------------------------
# Shared constants / helpers
# ---------------------------------------------------------------------------

WORKFLOW_COORDINATOR_SYSTEM = (
    "You are the workflow coordinator agent. Workflow steps run as "
    "sub-agents in this session; you do not need to delegate manually."
)

WORKFLOW_WORKER_SYSTEM = (
    "You are a focused workflow sub-agent. Complete the delegated task "
    "and return a concise result. No preamble or follow-up questions."
)

_worker_map: ContextVar[Dict[str, str]] = ContextVar(
    "workflow_oma_worker_map",
    default={},
)


def get_workflow_worker_map() -> Dict[str, str]:
    """Step name → OMA worker agent id for the current execution."""
    return _worker_map.get()


def build_multiagent_payload(
    worker_ids: List[str],
    *,
    versions: Optional[Dict[str, int]] = None,
) -> Dict[str, Any]:
    """Build coordinator multiagent roster payload."""
    agents: List[Dict[str, Any]] = []
    for worker_id in worker_ids:
        entry: Dict[str, Any] = {"type": "agent", "id": worker_id}
        version = (versions or {}).get(worker_id)
        if version is not None:
            entry["version"] = version
        agents.append(entry)
    return {"type": "coordinator", "agents": agents}


def collect_agent_steps(spec: Dict[str, Any]) -> List[Dict[str, Any]]:
    """Unique agent-based YAML steps (dedupe by name, action, model)."""
    seen: set[Tuple[str, str, str]] = set()
    steps: List[Dict[str, Any]] = []
    for step in spec.get("steps") or []:
        action = step.get("action")
        if action not in AGENT_ACTIONS:
            continue
        key = _step_dedup_key(step)
        if key in seen:
            continue
        seen.add(key)
        steps.append(step)
    return steps


def _step_dedup_key(step: Dict[str, Any]) -> Tuple[str, str, str]:
    params = step.get("params") or {}
    model = resolve_model(params) or harness_default_model()
    return (step["name"], step["action"], normalize_model_spec(model))


def _tool_configs_for_action(action: str) -> List[Dict[str, Any]]:
    names = TOOL_ALLOWLIST.get(action, [])
    if not names:
        return [{"type": "agent_toolset_20260401"}]
    configs = [{"name": name, "enabled": True} for name in names]
    return [{"type": "agent_toolset_20260401", "configs": configs}]


def _resolve_workdir(session_id: str) -> str:
    base = os.environ.get("SANDBOX_WORKDIR", "./data/sandboxes")
    path = Path(base).expanduser().resolve() / session_id
    path.mkdir(parents=True, exist_ok=True)
    return str(path)


def _platform_config() -> Tuple[str, str, str]:
    # Resolve base URL: OMA_API_BASE > OMA_PLATFORM_URL > localhost default.
    # Keeps harness consistent with OMAClient (sdk/oma_sdk/__init__.py).
    base = (
        os.environ.get("OMA_API_BASE")
        or os.environ.get("OMA_PLATFORM_URL")
        or "http://localhost:8787"
    ).rstrip("/")
    secret = os.environ.get("OMA_INTERNAL_SECRET", "")
    tenant = os.environ.get("OMA_TENANT_ID", "")
    return base, secret, tenant


def _agent_to_snapshot(agent: Any) -> Any:
    from pi_subagent.types import SubAgentSnapshot

    model_obj = getattr(agent, "model", None)
    if model_obj is not None and hasattr(model_obj, "id"):
        model_id = normalize_model_spec(str(model_obj.id))
    elif isinstance(model_obj, dict):
        model_id = normalize_model_spec(str(model_obj.get("id", harness_default_model())))
    else:
        model_id = normalize_model_spec(harness_default_model())

    system = getattr(agent, "system", None) or WORKFLOW_WORKER_SYSTEM
    version = int(getattr(agent, "version", 1) or 1)
    return SubAgentSnapshot(
        id=agent.id,
        name=agent.name,
        model=model_id,
        system_prompt=system,
        version=version,
    )


# ---------------------------------------------------------------------------
# OMA feature gate
# ---------------------------------------------------------------------------


def is_oma_bootstrap_enabled() -> bool:
    """True when we can create OMA entities AND configure the runtime bridge.

    Requires both ``oma_sdk`` (to create agents/sessions) and
    ``oma_adapter.subagent_bridge`` (to build the SubAgentRuntime that
    runs the agent steps).
    """
    if not is_oma_sdk_available():
        return False
    try:
        from oma_adapter.subagent_bridge import build_subagent_runtime  # noqa: F401
    except ImportError:
        return False
    return True


# ---------------------------------------------------------------------------
# OMA resource creation (SDK)
# ---------------------------------------------------------------------------


def _create_oma_resources_sync(
    *,
    workflow_name: str,
    execution_id: str,
    agent_steps: List[Dict[str, Any]],
    environment_id: Optional[str],
    tenant_id: Optional[str] = None,
) -> Tuple[str, str, Dict[str, str], Dict[str, Any], Dict[str, Any]]:
    """Create workers, coordinator, session via OMA SDK (sync)."""
    from oma_sdk import OMAClient

    exec_tag = execution_id[:8]
    prefix = f"workflow:{workflow_name[:32]}-{exec_tag}"

    client = OMAClient(tenant_id=tenant_id)
    if not environment_id:
        envs_resp = client.environments.list()
        envs = envs_resp.data if hasattr(envs_resp, "data") else []
        environment_id = envs[0].id if envs else "default"

    worker_ids: Dict[str, str] = {}
    worker_versions: Dict[str, int] = {}
    worker_snapshots: Dict[str, Any] = {}

    for step in agent_steps:
        step_name = step["name"]
        action = step["action"]
        params = step.get("params") or {}
        model = resolve_model(params) or harness_default_model()
        model_spec = {"id": normalize_model_spec(model)}
        tools = _tool_configs_for_action(action)

        worker = client.agents.create(
            name=f"{prefix}-worker-{step_name}",
            model=model_spec,
            system=WORKFLOW_WORKER_SYSTEM,
            description=f"Workflow worker for step {step_name} ({action})",
            tools=tools,
            metadata={
                "workflow_execution_id": execution_id,
                "workflow_step": step_name,
                "workflow_action": action,
            },
        )
        worker_ids[step_name] = worker.id
        worker_versions[worker.id] = int(getattr(worker, "version", 1) or 1)
        worker_snapshots[worker.id] = _agent_to_snapshot(worker)

    coordinator = client.agents.create(
        name=f"{prefix}-coordinator",
        model={"id": normalize_model_spec(harness_default_model())},
        system=WORKFLOW_COORDINATOR_SYSTEM,
        description=f"Workflow coordinator for {workflow_name}",
        metadata={"workflow_execution_id": execution_id},
        multiagent=build_multiagent_payload(
            list(worker_ids.values()),
            versions=worker_versions,
        ),
    )
    coordinator_snap = _agent_to_snapshot(coordinator)
    coordinator_snap = coordinator_snap.model_copy(
        update={"system_prompt": WORKFLOW_COORDINATOR_SYSTEM},
    )

    session = client.sessions.create(
        agent=coordinator.id,
        environment_id=environment_id,
        title=f"workflow:{workflow_name} #{exec_tag}",
    )

    return (
        session.id,
        coordinator.id,
        worker_ids,
        coordinator_snap.model_dump(),
        {aid: snap.model_dump() for aid, snap in worker_snapshots.items()},
    )


# ---------------------------------------------------------------------------
# Event bridge (internal OMA API)
# ---------------------------------------------------------------------------


async def _post_events_batch(
    session_id: str,
    events: List[Dict[str, Any]],
    *,
    enqueue: bool = False,
) -> None:
    base, secret, _tenant = _platform_config()
    if not secret:
        raise RuntimeError("OMA event bridge requires OMA_INTERNAL_SECRET")
    if not base:
        raise RuntimeError("OMA event bridge requires OMA_API_BASE or OMA_PLATFORM_URL")
    url = urljoin(base + "/", f"v1/internal/sessions/{session_id}/events/batch")
    async with httpx.AsyncClient(timeout=60.0) as client:
        resp = await client.post(
            url,
            headers={
                "Content-Type": "application/json",
                "x-internal-secret": secret,
            },
            json={"events": events, "enqueue": enqueue, "run_turn": False},
        )
    if resp.status_code >= 400:
        detail = resp.text.strip() or resp.reason_phrase
        raise RuntimeError(f"OMA events/batch failed: {detail}")


def _make_emit_event(session_id: str) -> Callable[[Dict[str, Any]], Awaitable[None]]:
    async def emit_event(event: Dict[str, Any]) -> None:
        await _post_events_batch(session_id, [event], enqueue=False)

    return emit_event


# ---------------------------------------------------------------------------
# Metadata keys used to thread OMA-specific tokens through the Protocol ctx
# ---------------------------------------------------------------------------

_META_RUNTIME_TOKEN = "runtime_token"
_META_WORKER_MAP_TOKEN = "worker_map_token"


def _ctx_to_metadata(
    ctx: WorkflowBootstrapContext,
) -> Dict[str, Any]:
    """Pull OMA-specific tokens out of ``ctx.metadata`` (returns new dict)."""
    return {
        _META_RUNTIME_TOKEN: ctx.metadata.get(_META_RUNTIME_TOKEN),
        _META_WORKER_MAP_TOKEN: ctx.metadata.get(_META_WORKER_MAP_TOKEN),
    }


# ---------------------------------------------------------------------------
# OmaWorkflowBootstrap — implements WorkflowBootstrap Protocol
# ---------------------------------------------------------------------------


class OmaWorkflowBootstrap:
    """Bootstrap workflow executions against the OMA platform.

    At ``setup()`` time:
      1. Create OMA worker agents (one per YAML step) + coordinator via SDK.
      2. Create a session bound to the coordinator.
      3. Configure ``SubAgentRuntime`` so workflow agent() steps delegate to
         the harness sandbox, where the coordinator roster drives execution.
    At ``teardown()`` time:
      Reset the runtime ContextVar so subsequent turns run on the primary
      (non-workflow) runtime again.
    """

    async def setup(
        self,
        *,
        workflow_name: str,
        execution_id: str,
        spec: Dict[str, Any],
        environment_id: Optional[str] = None,
        tenant_id: Optional[str] = None,
    ) -> Optional[WorkflowBootstrapContext]:
        if not is_oma_bootstrap_enabled():
            logger.debug("OMA bootstrap skipped (harness bridge unavailable)")
            return None

        agent_steps = collect_agent_steps(spec)
        if not agent_steps:
            logger.debug("OMA bootstrap skipped (no agent-based steps)")
            return None

        try:
            (
                session_id,
                coordinator_id,
                worker_ids,
                coordinator_data,
                workers_data,
            ) = await asyncio.to_thread(
                _create_oma_resources_sync,
                workflow_name=workflow_name,
                execution_id=execution_id,
                agent_steps=agent_steps,
                environment_id=environment_id,
                tenant_id=tenant_id,
            )
        except Exception as exc:
            logger.exception("OMA SDK bootstrap failed: %s", exc)
            return None

        from oma_adapter.subagent_bridge import build_subagent_runtime
        from oma_adapter.types import AgentSnapshot
        from pi_subagent.runtime import configure_subagent_runtime
        from pi_subagent.types import SubAgentSnapshot

        coordinator = SubAgentSnapshot.model_validate(coordinator_data)
        sub_agents = {
            aid: SubAgentSnapshot.model_validate(data)
            for aid, data in workers_data.items()
        }
        workdir = _resolve_workdir(session_id)
        _base, _secret, platform_tenant_id = _platform_config()
        # Use tenant_id from parameter if provided, otherwise fall back to platform config
        effective_tenant_id = tenant_id or platform_tenant_id
        emit_event = _make_emit_event(session_id)

        parent_oma = AgentSnapshot.model_validate(coordinator.model_dump())
        sub_oma = {
            aid: AgentSnapshot.model_validate(s.model_dump())
            for aid, s in sub_agents.items()
        }

        runtime = build_subagent_runtime(
            session_id=session_id,
            tenant_id=effective_tenant_id,
            workdir=workdir,
            parent_agent=parent_oma,
            sub_agents=sub_oma,
            model=None,
            aux_model=None,
            environment=None,
            emit_event=emit_event,
            mcp_proxy_base=os.environ.get("MCP_PROXY_BASE"),
            mcp_proxy_api_key=os.environ.get("MCP_PROXY_API_KEY"),
            outbound_proxy_addr=os.environ.get("OUTBOUND_PROXY_ADDR"),
            outbound_proxy_api_key=os.environ.get("OUTBOUND_PROXY_API_KEY"),
        )

        runtime_token = configure_subagent_runtime(runtime)
        worker_map_token = _worker_map.set(worker_ids)

        workflow_title = f"workflow:{workflow_name} #{execution_id[:8]}"
        try:
            await _post_events_batch(
                session_id,
                [
                    {
                        "type": "user.message",
                        "session_thread_id": "sthr_primary",
                        "content": [
                            {
                                "type": "text",
                                "text": (
                                    f"Workflow execution started: {workflow_title}. "
                                    f"Agent steps will run as sub-agent threads."
                                ),
                            },
                        ],
                    },
                ],
                enqueue=False,
            )
        except Exception as exc:
            logger.warning("Failed to post workflow start event: %s", exc)

        logger.info(
            "OMA bootstrap ready session=%s coordinator=%s workers=%d",
            session_id,
            coordinator_id,
            len(worker_ids),
        )

        return WorkflowBootstrapContext(
            session_id=session_id,
            coordinator_id=coordinator_id,
            worker_ids=dict(worker_ids),
            metadata={
                _META_RUNTIME_TOKEN: runtime_token,
                _META_WORKER_MAP_TOKEN: worker_map_token,
            },
            enabled=True,
        )

    async def teardown(self, ctx: Optional[WorkflowBootstrapContext]) -> None:
        if ctx is None or not ctx.enabled:
            return

        meta = _ctx_to_metadata(ctx) if ctx.metadata else {}
        runtime_token = meta.get(_META_RUNTIME_TOKEN)
        worker_map_token = meta.get(_META_WORKER_MAP_TOKEN)

        try:
            from pi_subagent.runtime import reset_subagent_runtime

            if runtime_token is not None:
                reset_subagent_runtime(runtime_token)
        except ImportError:
            pass

        if worker_map_token is not None:
            _worker_map.reset(worker_map_token)
        else:
            _worker_map.set({})
