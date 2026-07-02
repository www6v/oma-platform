"""Custom-tool HITL helpers (gate cookbook GT1/GT2).

Mirrors open-managed-agents ``default-loop.ts`` tool classification:
built-in / MCP / subagent tools → ``agent.tool_use``; everything else →
``agent.custom_tool_use``.
"""

from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from oma_adapter.tools import OMA_DEFAULT_TOOLS, TEAM_TOOL_NAMES
from oma_adapter.types import AgentSnapshot

# Cookbook parallel custom-tool window (Anthropic managed agents).
MAX_PENDING_EVENT_IDS = 5

# open-managed-agents harness/tools.ts ALL_TOOLS parity subset.
_WIRE_BUILTIN_NAMES = frozenset(OMA_DEFAULT_TOOLS) | frozenset(
    {"browser", "ls", "find"}
)


@dataclass(frozen=True)
class CustomToolDef:
    """Agent-declared custom tool (``type: custom``)."""

    name: str
    description: str
    input_schema: dict[str, Any]


def custom_tools_from_agent(agent: AgentSnapshot) -> list[CustomToolDef]:
    """Return explicit ``type: custom`` tools from an agent snapshot."""
    out: list[CustomToolDef] = []
    for item in agent.tools or []:
        if not isinstance(item, dict):
            continue
        if item.get("type") != "custom":
            continue
        name = item.get("name")
        if not isinstance(name, str) or not name.strip():
            continue
        schema = item.get("input_schema")
        if not isinstance(schema, dict):
            schema = {}
        desc = item.get("description")
        out.append(
            CustomToolDef(
                name=name.strip(),
                description=str(desc or ""),
                input_schema=schema,
            )
        )
    return out


def custom_tool_names(agent: AgentSnapshot) -> frozenset[str]:
    """Names declared as ``type: custom`` on the agent."""
    return frozenset(tool.name for tool in custom_tools_from_agent(agent))


def is_wire_builtin_tool(name: str) -> bool:
    """True when the tool maps to ``agent.tool_use`` (not custom round-trip)."""
    if not name:
        return False
    if name in _WIRE_BUILTIN_NAMES:
        return True
    if name in TEAM_TOOL_NAMES:
        return True
    if name.startswith("mcp__") or name.startswith("mcp_"):
        return True
    if name.startswith("call_agent_"):
        return True
    return False


def wire_tool_use_type(name: str) -> str:
    """Return ``agent.tool_use`` or ``agent.custom_tool_use`` for a tool name."""
    if is_wire_builtin_tool(name):
        return "agent.tool_use"
    return "agent.custom_tool_use"


def pending_custom_tool_ids(events: list[dict[str, Any]]) -> list[str]:
    """IDs of custom tool uses without a matching ``agent.tool_result``."""
    uses: dict[str, int] = {}
    answered: set[str] = set()
    order: list[str] = []

    for event in events:
        if not isinstance(event, dict):
            continue
        typ = event.get("type")
        if typ == "agent.custom_tool_use":
            tool_id = str(event.get("id") or "")
            if tool_id and tool_id not in uses:
                uses[tool_id] = 1
                order.append(tool_id)
        elif typ == "agent.tool_use":
            name = str(event.get("name") or "")
            if is_wire_builtin_tool(name):
                continue
            tool_id = str(event.get("id") or "")
            if tool_id and tool_id not in uses:
                uses[tool_id] = 1
                order.append(tool_id)
        elif typ == "agent.tool_result":
            tool_use_id = str(event.get("tool_use_id") or "")
            if tool_use_id:
                answered.add(tool_use_id)

    pending = [tid for tid in order if tid not in answered]
    if len(pending) > MAX_PENDING_EVENT_IDS:
        return pending[:MAX_PENDING_EVENT_IDS]
    return pending


def make_custom_tool_stub(defn: CustomToolDef) -> Any:
    """Build a piPy tool stub: schema for the model, no OMA tool_result."""
    from pi_agent.types import AgentToolResult
    from pi_ai.types import TextContent

    schema = defn.input_schema
    if not isinstance(schema, dict) or not schema:
        schema = {"type": "object", "properties": {}}

    class CustomToolStub:
        name = defn.name
        description = defn.description or f"Custom tool {defn.name}"
        parameters = schema
        execution_mode = "parallel"

        async def execute(
            self,
            tool_call_id: str,
            args: dict[str, Any],
            signal: Any = None,
            on_update: Any = None,
        ) -> AgentToolResult:
            del tool_call_id, args, signal, on_update
            # No execute on managed-agents — client supplies result via HITL.
            return AgentToolResult(content=[TextContent(text="")], is_error=False)

    return CustomToolStub()


def register_custom_tools_on_session(
    session: Any,
    defs: list[CustomToolDef],
) -> None:
    """Attach custom tool stubs after session creation (MCP registration parity)."""
    if not defs:
        return
    agent = getattr(session, "_agent", None)
    if agent is None:
        return
    tools = getattr(agent, "_tools", None)
    if tools is None:
        return
    existing = {getattr(tool, "name", "") for tool in tools}
    new_tools = [
        make_custom_tool_stub(defn)
        for defn in defs
        if defn.name not in existing
    ]
    if not new_tools:
        return
    agent._tools = [*tools, *new_tools]
    resources = getattr(session, "_resources", None)
    if resources is not None and hasattr(resources, "extension_runtime"):
        resources.extension_runtime.tools.extend(new_tools)

