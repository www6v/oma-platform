"""Per-turn runtime for web_search (outbound proxy, backend, Tavily key)."""

from __future__ import annotations

import os
from dataclasses import dataclass
from typing import Any

from oma_adapter.types import AgentSnapshot


@dataclass
class WebSearchRuntime:
    environment: dict[str, Any] | None = None
    outbound_proxy_url: str | None = None
    outbound_proxy_api_key: str | None = None
    session_id: str | None = None
    backend: str = "ddg"
    tavily_api_key: str | None = None


_runtime: WebSearchRuntime | None = None


def _tool_types(agent: AgentSnapshot) -> set[str]:
    types: set[str] = set()
    for item in agent.tools or []:
        if isinstance(item, dict) and item.get("type"):
            types.add(str(item["type"]))
    return types


def resolve_search_backend(agent: AgentSnapshot) -> str:
    """Mirror harness/tools.ts: Tavily type overrides DDG when present.

    Also use Tavily if TAVILY_API_KEY is set (for China deployment where
    DuckDuckGo is blocked by GFW).
    """
    if "web_search_tavily" in _tool_types(agent):
        return "tavily"
    # Fallback: if TAVILY_API_KEY is set, prefer Tavily over DDG
    if os.environ.get("TAVILY_API_KEY"):
        return "tavily"
    return "ddg"


def configure_web_search(runtime: WebSearchRuntime) -> None:
    global _runtime
    _runtime = runtime


def get_web_search_runtime() -> WebSearchRuntime:
    if _runtime is not None:
        return _runtime
    return WebSearchRuntime(
        backend="ddg",
        tavily_api_key=os.environ.get("TAVILY_API_KEY"),
    )


def clear_web_search_runtime() -> None:
    global _runtime
    _runtime = None
