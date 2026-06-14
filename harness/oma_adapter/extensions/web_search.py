"""OMA web_search tool registered via piPy extension (harness/tools.ts parity)."""

from __future__ import annotations

from typing import Any

from pi_agent.types import AgentToolResult
from pi_ai.types import TextContent

from oma_adapter.web_search.core import DEFAULT_MAX_RESULTS, search_web


class WebSearchTool:
    """Search the web via DuckDuckGo or Tavily (when configured)."""

    name = "web_search"
    description = (
        "Search the web. Default backend is DuckDuckGo (titles, URLs, "
        "descriptions). Agents with web_search_tavily use Tavily when "
        "TAVILY_API_KEY is set."
    )
    parameters: dict[str, Any] = {
        "type": "object",
        "properties": {
            "query": {"type": "string", "description": "Search query"},
            "max_results": {
                "type": "integer",
                "description": (
                    "Max results to return (default "
                    f"{DEFAULT_MAX_RESULTS}, max 20)"
                ),
            },
        },
        "required": ["query"],
    }
    execution_mode = "parallel"

    async def execute(
        self,
        tool_call_id: str,
        args: dict[str, Any],
        signal: Any = None,
        on_update: Any = None,
    ) -> AgentToolResult:
        del tool_call_id, signal, on_update
        query_raw = args.get("query")
        if not isinstance(query_raw, str) or not query_raw.strip():
            return AgentToolResult(
                content=[TextContent(text="Error: query is required")],
                is_error=True,
            )

        max_results_raw = args.get("max_results")
        max_results: int | None = None
        if max_results_raw is not None:
            try:
                max_results = int(max_results_raw)
            except (TypeError, ValueError):
                max_results = None

        try:
            text = await search_web(query_raw.strip(), max_results)
        except Exception as exc:  # noqa: BLE001 — tool errors return to model
            return AgentToolResult(
                content=[TextContent(text=f"Error: {exc}")],
                is_error=True,
            )

        is_error = text.startswith("Error:") or text.startswith(
            "web_search unavailable:"
        )
        return AgentToolResult(
            content=[TextContent(text=text)],
            is_error=is_error,
        )


def register(pi: Any) -> None:
    """piPy extension entrypoint."""
    pi.register_tool(WebSearchTool())
