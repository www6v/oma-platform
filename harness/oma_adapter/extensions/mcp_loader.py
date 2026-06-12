"""Register MCP tools discovered for the current turn."""

from __future__ import annotations

from typing import Any

from oma_adapter.mcp.runtime import get_mcp_runtime


def register(pi: Any) -> None:
    runtime = get_mcp_runtime()
    if runtime is None:
        return
    for tool in runtime.tools:
        pi.register_tool(tool)
