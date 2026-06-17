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
    for entry in parent.callable_agents or []:
        api.register_tool(make_call_agent_tool(entry.id)())

    if parent.enable_general_subagent:
        api.register_tool(GeneralSubagentTool())
