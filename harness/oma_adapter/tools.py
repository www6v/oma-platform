"""Map OMA agent tool declarations to piPy tool names."""

from __future__ import annotations

from typing import Any

from oma_adapter.types import AgentSnapshot

# OMA agent_toolset_20260401 names that piPy can satisfy today.
OMA_DEFAULT_TOOLS = [
    "bash",
    "read",
    "write",
    "edit",
    "glob",
    "grep",
]

# OMA name -> piPy builtin (glob has no piPy equivalent; find covers it).
OMA_TO_PIPY: dict[str, str] = {
    "bash": "bash",
    "read": "read",
    "write": "write",
    "edit": "edit",
    "glob": "find",
    "grep": "grep",
    "ls": "ls",
    "find": "find",
}

PIPY_TOOL_ORDER = ["bash", "read", "write", "edit", "grep", "find", "ls"]

_DEFAULT_OMA_SET = {OMA_TO_PIPY[name] for name in OMA_DEFAULT_TOOLS if name in OMA_TO_PIPY}
DEFAULT_PIPY_TOOLS = [name for name in PIPY_TOOL_ORDER if name in _DEFAULT_OMA_SET]


def _pipy_name(raw: str) -> str | None:
    """Resolve an OMA or piPy tool name to a supported piPy builtin."""
    mapped = OMA_TO_PIPY.get(raw, raw)
    if mapped in PIPY_TOOL_ORDER:
        return mapped
    return None


def _ordered_pipy_names(names: set[str]) -> list[str]:
    return [name for name in PIPY_TOOL_ORDER if name in names]


def _enabled_oma_tools(toolset: dict[str, Any]) -> set[str]:
    """Mirror OMA getEnabledTools() for agent_toolset_20260401."""
    default_set = set(OMA_DEFAULT_TOOLS)
    configs = toolset.get("configs")
    if not isinstance(configs, list):
        return default_set

    enabled: set[str] = set()
    default_cfg = toolset.get("default_config")
    default_enabled = True
    if isinstance(default_cfg, dict) and "enabled" in default_cfg:
        default_enabled = bool(default_cfg["enabled"])

    if default_enabled:
        enabled.update(default_set)

    for entry in configs:
        if not isinstance(entry, dict):
            continue
        name = entry.get("name")
        if not isinstance(name, str):
            continue
        if entry.get("enabled"):
            enabled.add(name)
        else:
            enabled.discard(name)

    return enabled


def _pipy_from_toolset(toolset: dict[str, Any]) -> list[str]:
    oma_enabled = _enabled_oma_tools(toolset)
    pipy_names: set[str] = set()
    for oma_name in oma_enabled:
        pipy = _pipy_name(oma_name)
        if pipy is not None:
            pipy_names.add(pipy)
    return _ordered_pipy_names(pipy_names)


def pypi_tools_from_agent(agent: AgentSnapshot) -> list[str]:
    """Resolve piPy tool list from an OMA agent snapshot."""
    if not agent.tools:
        return list(DEFAULT_PIPY_TOOLS)

    pipy_names: set[str] = set()
    saw_toolset = False

    for item in agent.tools:
        if not isinstance(item, dict):
            continue
        tool_type = item.get("type")
        if tool_type == "agent_toolset_20260401":
            saw_toolset = True
            pipy_names.update(_pipy_from_toolset(item))
            continue
        raw_name = item.get("name")
        if isinstance(raw_name, str):
            pipy = _pipy_name(raw_name)
            if pipy is not None:
                pipy_names.add(pipy)

    if saw_toolset:
        return _ordered_pipy_names(pipy_names)

    if pipy_names:
        return _ordered_pipy_names(pipy_names)

    return list(DEFAULT_PIPY_TOOLS)
