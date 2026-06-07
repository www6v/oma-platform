"""Map OMA agent tool declarations to piPy tool names."""

from __future__ import annotations

from typing import Any

from oma_adapter.types import AgentSnapshot

DEFAULT_PIPY_TOOLS = ["read", "bash", "write"]


def pypi_tools_from_agent(agent: AgentSnapshot) -> list[str]:
    """Resolve piPy tool list from an OMA agent snapshot."""
    if not agent.tools:
        return list(DEFAULT_PIPY_TOOLS)

    names: list[str] = []
    for item in agent.tools:
        if not isinstance(item, dict):
            continue
        tool_type = item.get("type")
        if tool_type == "agent_toolset_20260401":
            names.extend(DEFAULT_PIPY_TOOLS)
            continue
        if isinstance(item.get("name"), str):
            names.append(item["name"])

    if not names:
        return list(DEFAULT_PIPY_TOOLS)
    return names
