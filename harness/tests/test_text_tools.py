"""Tests for gateway-style text tool call parsing."""

import pytest

from oma_adapter.text_tools import (
    normalize_text_tool_name,
    parse_text_tool_calls,
)


def test_normalize_anonymous_mcp_alias() -> None:
    assert normalize_text_tool_name("anonymous__smoke__ping") == (
        "mcp__smoke__ping"
    )


def test_parse_text_tool_call_block() -> None:
    text = "$$anonymous__smoke__ping$$\n{}\n$$end$$\n\npong"
    calls = parse_text_tool_calls(text)
    assert calls == [("mcp__smoke__ping", {})]


def test_parse_text_tool_call_with_args() -> None:
    text = '$$mcp__linear__search_issues$$\n{"q": "bug"}\n$$end$$'
    calls = parse_text_tool_calls(text)
    assert calls == [("mcp__linear__search_issues", {"q": "bug"})]


def test_parse_json_dollar_tool_call() -> None:
    text = '$${"toolName":"mcp__smoke__ping","arguments":{}}$$\n\npong'
    calls = parse_text_tool_calls(text)
    assert calls == [("mcp__smoke__ping", {})]

    from oma_adapter.text_tools import mcp_tools_named_in_text

    prompt = "Call the MCP tool named mcp__smoke__ping with empty arguments."
    assert mcp_tools_named_in_text(prompt) == ["mcp__smoke__ping"]


@pytest.mark.asyncio
async def test_execute_prompted_mcp_tools_uses_runtime_tools() -> None:
    from pi_agent.types import AgentToolResult
    from pi_ai.types import TextContent

    from oma_adapter.mcp.runtime import McpRuntime
    from oma_adapter.text_tools import execute_prompted_mcp_tools

    class PingTool:
        name = "mcp__smoke__ping"

        async def execute(
            self,
            tool_call_id: str,
            args: dict,
            signal: object = None,
            on_update: object = None,
        ) -> AgentToolResult:
            del tool_call_id, args, signal, on_update
            return AgentToolResult(
                content=[TextContent(text="pong-from-mcp-smoke")],
                is_error=False,
            )

    class FakeAgent:
        _tools: list[object] = []

    class FakeSession:
        _agent = FakeAgent()

    events = await execute_prompted_mcp_tools(
        FakeSession(),
        prompt_text=(
            "Call the MCP tool named mcp__smoke__ping with empty arguments."
        ),
        mcp_runtime=McpRuntime(tools=[PingTool()]),
    )
    assert any(ev.get("type") == "agent.tool_use" for ev in events)
    assert any(
        ev.get("type") == "agent.tool_result"
        and "pong-from-mcp-smoke" in (ev.get("content") or [{}])[0].get(
            "text", ""
        )
        for ev in events
    )
