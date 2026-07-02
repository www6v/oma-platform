"""Register agent-declared custom tools for gate HITL (GT1).

Stub tools expose JSON Schema to the model; results round-trip via
``user.custom_tool_result`` (no local ``agent.tool_result``).
"""

from __future__ import annotations

from typing import Any

from oma_adapter.custom_tools import make_custom_tool_stub
from oma_adapter.custom_tools_runtime import get_custom_tools_runtime


def register(pi: Any) -> None:
    """piPy extension entrypoint."""
    runtime = get_custom_tools_runtime()
    if runtime is None or not runtime.tools:
        return
    for defn in runtime.tools:
        pi.register_tool(make_custom_tool_stub(defn))
