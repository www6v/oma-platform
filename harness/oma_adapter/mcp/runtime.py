"""Per-turn MCP tool runtime (mirrors web_fetch.runtime)."""

from __future__ import annotations

from dataclasses import dataclass, field
from typing import Any

_runtime: McpRuntime | None = None


@dataclass
class McpRuntime:
    """Tools discovered for the current harness turn."""

    tools: list[Any] = field(default_factory=list)


def configure_mcp_runtime(runtime: McpRuntime | None) -> None:
    global _runtime
    _runtime = runtime


def get_mcp_runtime() -> McpRuntime | None:
    return _runtime


def clear_mcp_runtime() -> None:
    global _runtime
    _runtime = None
