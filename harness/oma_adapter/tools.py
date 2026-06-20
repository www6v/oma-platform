"""Map OMA agent tool declarations to piPy tool names."""

from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from oma_adapter.types import AgentSnapshot

# OMA agent_toolset_20260401 defaults (open-managed-agents harness/tools.ts).
OMA_DEFAULT_TOOLS = [
    "bash",
    "read",
    "write",
    "edit",
    "glob",
    "grep",
    "web_fetch",
    "web_search",
    "schedule",
    "cancel_schedule",
    "list_schedules",
]

# OMA name -> piPy builtin (glob has no piPy equivalent; find covers it).
OMA_TO_PIPY: dict[str, str] = {
    "bash": "bash",
    "read": "read",
    "write": "write",
    "edit": "edit",
    "glob": "find",
    "grep": "grep",
    "web_fetch": "web_fetch",
    "web_search": "web_search",
    "ls": "ls",
    "find": "find",
}

PIPY_BUILTIN_ORDER = ["bash", "read", "write", "edit", "grep", "find", "ls"]
PIPY_EXTENSION_ORDER = [
    "web_fetch",
    "web_search",
    "schedule",
    "cancel_schedule",
    "list_schedules",
]
PIPY_TOOL_ORDER = [*PIPY_BUILTIN_ORDER, *PIPY_EXTENSION_ORDER]

WEB_FETCH_EXTENSION_PATH = (
    Path(__file__).resolve().parent / "extensions" / "web_fetch.py"
)
WEB_SEARCH_EXTENSION_PATH = (
    Path(__file__).resolve().parent / "extensions" / "web_search.py"
)
SCHEDULE_EXTENSION_PATH = (
    Path(__file__).resolve().parent / "extensions" / "schedule.py"
)
MCP_LOADER_EXTENSION_PATH = (
    Path(__file__).resolve().parent / "extensions" / "mcp_loader.py"
)
TEAM_EXTENSION_PATH = (
    Path(__file__).resolve().parent / "extensions" / "team_extension.py"
)
SUBAGENT_EXTENSION_PATH = (
    Path(__file__).resolve().parent / "extensions" / "subagent_extension.py"
)


def resolve_subagent_extension_path() -> Path:
    """Resolve piPy subagent extension path.

    Search order:
    1. PIPY_SUBAGENT_EXTENSION env var (explicit override)
    2. Bundled copy inside oma_adapter/extensions/ (Docker / installed)
    3. Sibling repo piPy-subagent/extensions/ (local dev)
    """
    explicit = os.environ.get("PIPY_SUBAGENT_EXTENSION")
    if explicit:
        return Path(explicit)
    if SUBAGENT_EXTENSION_PATH.is_file():
        return SUBAGENT_EXTENSION_PATH
    return (
        Path(__file__).resolve().parents[3]
        / "piPy-subagent"
        / "extensions"
        / "subagent_extension.py"
    )


def resolve_teams_extension_path() -> Path:
    """Resolve piPy teams extension path.

    Search order:
    1. PIPY_TEAMS_EXTENSION env var (explicit override)
    2. Bundled copy inside oma_adapter/extensions/ (Docker / installed)
    3. Sibling repo piPy-teams/extensions/ (local dev)
    """
    explicit = os.environ.get("PIPY_TEAMS_EXTENSION")
    if explicit:
        return Path(explicit)
    if TEAM_EXTENSION_PATH.is_file():
        return TEAM_EXTENSION_PATH
    return (
        Path(__file__).resolve().parents[3]
        / "piPy-teams"
        / "extensions"
        / "team_extension.py"
    )

TEAM_TOOL_NAMES = frozenset(
    {
        "team_create",
        "spawn_teammate",
        "send_team_message",
        "read_team_messages",
    }
)

OMA_EXTENSION_TOOLS = frozenset(
    {
        "web_fetch",
        "web_search",
        "schedule",
        "cancel_schedule",
        "list_schedules",
    }
)
SCHEDULE_TOOL_NAMES = frozenset(
    {"schedule", "cancel_schedule", "list_schedules"}
)
PIPY_BUILTIN_NAMES = frozenset(PIPY_BUILTIN_ORDER)

_DEFAULT_OMA_SET = {OMA_TO_PIPY[name] for name in OMA_DEFAULT_TOOLS if name in OMA_TO_PIPY}
_DEFAULT_OMA_SET.update(
    name for name in OMA_DEFAULT_TOOLS if name in OMA_EXTENSION_TOOLS
)
DEFAULT_PIPY_TOOLS = [name for name in PIPY_TOOL_ORDER if name in _DEFAULT_OMA_SET]


@dataclass(frozen=True)
class SessionToolConfig:
    """Resolved piPy session tool wiring for a turn."""

    builtin_tools: list[str]
    extension_paths: list[str]


def _extension_paths_for_names(names: set[str]) -> list[str]:
    paths: list[str] = []
    if "web_fetch" in names and WEB_FETCH_EXTENSION_PATH.is_file():
        paths.append(str(WEB_FETCH_EXTENSION_PATH))
    if "web_search" in names and WEB_SEARCH_EXTENSION_PATH.is_file():
        paths.append(str(WEB_SEARCH_EXTENSION_PATH))
    if names.intersection(SCHEDULE_TOOL_NAMES) and SCHEDULE_EXTENSION_PATH.is_file():
        paths.append(str(SCHEDULE_EXTENSION_PATH))
    return paths


def _web_search_type_tools(agent: AgentSnapshot) -> set[str]:
    """Tool types that imply web_search (harness/tools.ts parity)."""
    types: set[str] = set()
    for item in agent.tools or []:
        if not isinstance(item, dict):
            continue
        tool_type = item.get("type")
        if tool_type in ("web_search_tavily", "web_search_ddg"):
            types.add("web_search")
    return types


def _extension_paths_for_agent(agent: AgentSnapshot) -> list[str]:
    import logging
    _logger = logging.getLogger("oma.tools")
    paths = _extension_paths_for_names(_resolved_tool_names(agent))
    if agent.mcp_servers and MCP_LOADER_EXTENSION_PATH.is_file():
        paths.append(str(MCP_LOADER_EXTENSION_PATH))
    subagent_path = resolve_subagent_extension_path()
    if _needs_subagent_extension(agent) and subagent_path.is_file():
        paths.append(str(subagent_path))
    needs_team = _needs_team_extension(agent)
    team_ext_path = resolve_teams_extension_path()
    _logger.warning(
        "[tools] _extension_paths_for_agent: enable_team_tools=%s needs_team=%s team_path=%s is_file=%s",
        agent.enable_team_tools,
        needs_team,
        team_ext_path,
        team_ext_path.is_file(),
    )
    if needs_team and team_ext_path.is_file():
        paths.append(str(team_ext_path))
    return paths


def _needs_team_extension(agent: AgentSnapshot) -> bool:
    if agent.enable_team_tools:
        return True
    for item in agent.tools or []:
        if not isinstance(item, dict):
            continue
        name = item.get("name")
        if isinstance(name, str) and name in TEAM_TOOL_NAMES:
            return True
    return False


def _needs_subagent_extension(agent: AgentSnapshot) -> bool:
    if agent.callable_agents:
        return True
    if agent.enable_general_subagent:
        return True
    try:
        from pi_subagent.roles_resolve import has_default_subagent_roles
        return has_default_subagent_roles(agent.metadata)
    except ImportError:
        # pi_subagent not available, assume no default roles
        return False


def _pipy_name(raw: str) -> str | None:
    """Resolve an OMA or piPy tool name to a supported harness tool."""
    mapped = OMA_TO_PIPY.get(raw, raw)
    if mapped in PIPY_BUILTIN_NAMES or mapped in OMA_EXTENSION_TOOLS:
        return mapped
    return None


def _ordered_pipy_names(names: set[str]) -> list[str]:
    return [name for name in PIPY_TOOL_ORDER if name in names]


def _enabled_oma_tools(toolset: dict[str, Any]) -> set[str]:
    """Mirror OMA getEnabledTools() for agent_toolset_20260401."""
    default_set = set(OMA_DEFAULT_TOOLS)
    default_cfg = toolset.get("default_config")
    default_enabled = True
    if isinstance(default_cfg, dict) and "enabled" in default_cfg:
        default_enabled = bool(default_cfg["enabled"])

    configs = toolset.get("configs")
    if not isinstance(configs, list):
        if default_enabled:
            return default_set
        return set()

    enabled: set[str] = set()
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


def _resolved_tool_names(agent: AgentSnapshot) -> set[str]:
    if not agent.tools:
        return set(DEFAULT_PIPY_TOOLS)

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

    if saw_toolset or pipy_names:
        pipy_names.update(_web_search_type_tools(agent))
        return pipy_names

    pipy_names.update(_web_search_type_tools(agent))
    if pipy_names:
        return pipy_names

    return set(DEFAULT_PIPY_TOOLS)


def enabled_schedule_tools(agent: AgentSnapshot) -> frozenset[str]:
    """Schedule tool names enabled for this agent snapshot."""
    return frozenset(_resolved_tool_names(agent)).intersection(SCHEDULE_TOOL_NAMES)


def pypi_tools_from_agent(agent: AgentSnapshot) -> list[str]:
    """Resolve enabled harness tool names from an OMA agent snapshot."""
    return _ordered_pipy_names(_resolved_tool_names(agent))


def session_tool_config_from_agent(agent: AgentSnapshot) -> SessionToolConfig:
    """Split enabled tools into piPy builtins vs extension module paths."""
    enabled = _resolved_tool_names(agent)
    builtin_tools = [
        name for name in PIPY_BUILTIN_ORDER if name in enabled
    ]
    extension_paths = _extension_paths_for_agent(agent)
    return SessionToolConfig(
        builtin_tools=builtin_tools,
        extension_paths=extension_paths,
    )
