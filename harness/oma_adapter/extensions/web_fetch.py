"""OMA web_fetch tool registered via piPy extension (harness/tools.ts parity)."""

from __future__ import annotations

from typing import Any

from pi_agent.types import AgentToolResult
from pi_ai.types import TextContent

from oma_adapter.web_fetch.core import DEFAULT_MAX_LENGTH, fetch_web_content


class WebFetchTool:
    """Fetch a URL and return page content as markdown-ish text."""

    name = "web_fetch"
    description = (
        "Fetch a URL and return clean markdown — strips boilerplate "
        "(nav/ads/scripts), preserves headings and links. Use this as the "
        "default way to read web pages; fall back to browser_* tools only if "
        "you need to interact (click, fill forms) or if the page is "
        "JS-rendered (SPA) and returns empty markdown. Large pages "
        "may be auto-summarized when an aux model is configured on the agent — "
        "the full raw markdown is then saved to /workspace/.web/, readable via "
        "the `read` tool when you need detail beyond the summary."
    )
    parameters: dict[str, Any] = {
        "type": "object",
        "properties": {
            "url": {"type": "string", "description": "URL to fetch"},
            "max_length": {
                "type": "integer",
                "description": (
                    "Truncate returned markdown to this many chars "
                    f"(default {DEFAULT_MAX_LENGTH})"
                ),
            },
        },
        "required": ["url"],
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
        url_raw = args.get("url")
        if not isinstance(url_raw, str) or not url_raw.strip():
            return AgentToolResult(
                content=[TextContent(text="Error: url is required")],
                is_error=True,
            )

        max_length_raw = args.get("max_length", DEFAULT_MAX_LENGTH)
        try:
            cap = int(max_length_raw)
        except (TypeError, ValueError):
            cap = DEFAULT_MAX_LENGTH

        try:
            text = await fetch_web_content(url_raw.strip(), cap)
        except Exception as exc:  # noqa: BLE001 — tool errors return to model
            return AgentToolResult(
                content=[TextContent(text=f"Error: {exc}")],
                is_error=True,
            )

        is_error = text.startswith("Error:")
        return AgentToolResult(
            content=[TextContent(text=text)],
            is_error=is_error,
        )


def register(pi: Any) -> None:
    """piPy extension entrypoint — mirrors pi ``registerTool`` pattern."""
    pi.register_tool(WebFetchTool())
