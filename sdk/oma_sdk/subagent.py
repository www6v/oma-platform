"""
Sub-agent configuration helpers for the OMA Python SDK.

Mirrors open-managed-agents eval runner patterns: create worker agents,
wire them into a coordinator via ``multiagent``, and observe delegation
through session threads / trajectory APIs.
"""

from __future__ import annotations

from typing import Any, Literal, TypedDict

SubagentRole = Literal["explore", "plan", "verify"]

DEFAULT_TOOLS: list[dict[str, str]] = [{"type": "agent_toolset_20260401"}]
DEFAULT_MODEL: dict[str, str] = {"id": "claude-sonnet-4-6"}


class CallableAgentRef(TypedDict, total=False):
    type: Literal["agent"]
    id: str
    version: int


class SubAgentConfig(TypedDict, total=False):
    """Worker agent definition — matches open-managed-agents eval ``subAgents``."""

    name: str
    system: str
    model: dict[str, str] | str
    tools: list[dict[str, Any]]


def build_multiagent(
    worker_ids: list[str],
    *,
    versions: dict[str, int] | None = None,
) -> dict[str, Any]:
    """Build AMA ``multiagent`` payload for a coordinator agent."""
    agents: list[dict[str, Any]] = []
    for worker_id in worker_ids:
        entry: dict[str, Any] = {"type": "agent", "id": worker_id}
        version = (versions or {}).get(worker_id)
        if version is not None:
            entry["version"] = version
        agents.append(entry)
    return {"type": "coordinator", "agents": agents}


def build_general_subagent_metadata() -> dict[str, Any]:
    """Metadata that enables the ``general_subagent`` delegation tool."""
    return {"enable_general_subagent": True}


def build_role_metadata(
    roles: dict[str, str],
) -> dict[str, Any]:
    """Metadata for ``default_subagent_roles`` (role → agent id)."""
    return {"default_subagent_roles": roles}


def event_type(event: Any) -> str | None:
    """Return the ``type`` field from a session event object or dict."""
    if isinstance(event, dict):
        return event.get("type")
    return getattr(event, "type", None)


def events_of_type(events: list[Any], event_type_name: str) -> list[Any]:
    """Filter session events by ``type``."""
    return [ev for ev in events if event_type(ev) == event_type_name]


def count_thread_created(events: list[Any]) -> int:
    """Count ``session.thread_created`` events (delegation occurred)."""
    return len(events_of_type(events, "session.thread_created"))


def extract_event_data(event: Any) -> dict[str, Any]:
    """Normalize a stored/list event to a plain dict."""
    if isinstance(event, dict):
        if "data" in event and isinstance(event["data"], dict):
            return event["data"]
        return event
    data = getattr(event, "model_dump", None)
    if callable(data):
        return data()
    if hasattr(event, "__dict__"):
        return {k: v for k, v in event.__dict__.items() if not k.startswith("_")}
    return {}
