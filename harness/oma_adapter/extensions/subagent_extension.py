"""piPy extension: call_agent_* and general_subagent tools.

Thin wiring layer — business logic lives in pi_subagent/.
Host must configure SubAgentRuntime before the extension loads (OMA turn).
"""

from __future__ import annotations

from typing import Any

from pi_subagent.runtime import get_subagent_runtime
from pi_subagent.tools import GeneralSubagentTool, make_call_agent_tool


def register(api: Any) -> None:
    runtime = get_subagent_runtime()
    if runtime is None:
        return

    parent = runtime.parent_agent
    registered: set[str] = set()

    for entry in parent.callable_agents or []:
        if not entry.id or entry.id in registered:
            continue
        api.register_tool(make_call_agent_tool(entry.id)())
        registered.add(entry.id)

    for agent_id in runtime.sub_agents:
        if agent_id in registered:
            continue
        api.register_tool(make_call_agent_tool(agent_id)())
        registered.add(agent_id)

    if parent.enable_general_subagent:
        api.register_tool(GeneralSubagentTool())
