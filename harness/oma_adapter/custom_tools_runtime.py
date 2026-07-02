"""Per-turn runtime for agent-declared custom tools (gate GT1)."""

from __future__ import annotations

from dataclasses import dataclass, field

from oma_adapter.custom_tools import CustomToolDef

_runtime: CustomToolsRuntime | None = None


@dataclass
class CustomToolsRuntime:
    """Custom tool defs active for the current harness turn."""

    tools: list[CustomToolDef] = field(default_factory=list)


def configure_custom_tools_runtime(runtime: CustomToolsRuntime | None) -> None:
    global _runtime
    _runtime = runtime


def get_custom_tools_runtime() -> CustomToolsRuntime | None:
    return _runtime


def clear_custom_tools_runtime() -> None:
    global _runtime
    _runtime = None
