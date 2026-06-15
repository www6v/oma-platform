"""Execute gateway-style text tool calls embedded in assistant messages.

Some Anthropic-compatible gateways (e.g. corporate proxies) emit tool invocations
as ``$$tool_name$$`` … ``$$end$$`` text instead of native tool_use blocks.
When the model returns that shape, run the matching harness tool and surface
OMA ``agent.tool_use`` / ``agent.tool_result`` events.
"""

from __future__ import annotations

import json
import re
import uuid
from typing import Any

from oma_adapter.mcp.runtime import McpRuntime

_TEXT_TOOL_RE = re.compile(
    r"\$\$([^$\n]+)\$\$\s*(\{[\s\S]*?\})?\s*\$\$end\$\$",
)
_JSON_TOOL_RE = re.compile(
    r"\$\$\s*(\{[\s\S]*?\})\s*\$\$",
)
_MCP_TOOL_NAME_RE = re.compile(r"mcp__\w+__\w+")


def normalize_text_tool_name(raw: str) -> str:
    """Map gateway tool aliases to harness tool names."""
    name = raw.strip()
    if name.startswith("anonymous__"):
        return f"mcp__{name[len('anonymous__'):]}"
    return name


def parse_text_tool_calls(text: str) -> list[tuple[str, dict[str, Any]]]:
    """Return (tool_name, args) pairs parsed from assistant text."""
    if not text or "$$" not in text:
        return []
    calls: list[tuple[str, dict[str, Any]]] = []
    seen: set[tuple[str, str]] = set()
    for match in _TEXT_TOOL_RE.finditer(text):
        raw_name = match.group(1).strip()
        args_raw = (match.group(2) or "{}").strip()
        try:
            args = json.loads(args_raw)
        except json.JSONDecodeError:
            args = {}
        if not isinstance(args, dict):
            args = {}
        key = (normalize_text_tool_name(raw_name), json.dumps(args, sort_keys=True))
        if key in seen:
            continue
        seen.add(key)
        calls.append((normalize_text_tool_name(raw_name), args))
    for match in _JSON_TOOL_RE.finditer(text):
        payload_raw = match.group(1).strip()
        try:
            payload = json.loads(payload_raw)
        except json.JSONDecodeError:
            continue
        if not isinstance(payload, dict):
            continue
        raw_name = (
            payload.get("toolName")
            or payload.get("tool_name")
            or payload.get("name")
        )
        if not isinstance(raw_name, str) or not raw_name.strip():
            continue
        args = (
            payload.get("arguments")
            or payload.get("args")
            or payload.get("input")
            or {}
        )
        if not isinstance(args, dict):
            args = {}
        key = (normalize_text_tool_name(raw_name), json.dumps(args, sort_keys=True))
        if key in seen:
            continue
        seen.add(key)
        calls.append((normalize_text_tool_name(raw_name), args))
    return calls


def mcp_tools_named_in_text(text: str) -> list[str]:
    """Extract harness MCP tool names referenced in free text."""
    if not text:
        return []
    return _MCP_TOOL_NAME_RE.findall(text)


def _tools_by_name(
    session: Any,
    *,
    fallback_tools: list[Any] | None = None,
) -> dict[str, Any]:
    tools_by_name = {
        getattr(tool, "name", ""): tool
        for tool in session._agent._tools
        if getattr(tool, "name", "")
    }
    for tool in fallback_tools or []:
        name = getattr(tool, "name", "")
        if name and name not in tools_by_name:
            tools_by_name[name] = tool
    return tools_by_name


async def _run_tool_call(
    session: Any,
    *,
    tool_name: str,
    args: dict[str, Any],
    fallback_tools: list[Any] | None = None,
) -> list[dict[str, Any]]:
    tool = _tools_by_name(session, fallback_tools=fallback_tools).get(tool_name)
    if tool is None:
        return []

    tool_call_id = f"tool_{uuid.uuid4().hex[:12]}"
    events: list[dict[str, Any]] = [
        {
            "type": "agent.tool_use",
            "id": tool_call_id,
            "name": tool_name,
            "input": args,
        }
    ]
    try:
        result = await tool.execute(tool_call_id, args)
    except Exception as exc:  # noqa: BLE001
        result_text = f"Error: {exc}"
        is_error = True
    else:
        result_text = _tool_result_text(result)
        is_error = bool(getattr(result, "is_error", False))
    events.append(
        {
            "type": "agent.tool_result",
            "tool_use_id": tool_call_id,
            "content": [{"type": "text", "text": result_text}],
            "is_error": is_error,
        }
    )
    return events


async def execute_text_tool_calls(
    session: Any,
    *,
    assistant_text: str,
    fallback_tools: list[Any] | None = None,
) -> list[dict[str, Any]]:
    """Run parsed text tool calls; return OMA tool_use/tool_result events."""
    calls = parse_text_tool_calls(assistant_text)
    oma_events: list[dict[str, Any]] = []
    for tool_name, args in calls:
        oma_events.extend(
            await _run_tool_call(
                session,
                tool_name=tool_name,
                args=args,
                fallback_tools=fallback_tools,
            )
        )
    return oma_events


async def execute_prompted_mcp_tools(
    session: Any,
    *,
    prompt_text: str,
    mcp_runtime: McpRuntime,
) -> list[dict[str, Any]]:
    """Run MCP tools explicitly named in the user prompt when the model skipped them."""
    if not mcp_runtime.tools:
        return []
    available = {getattr(tool, "name", "") for tool in mcp_runtime.tools}
    requested = [
        name
        for name in dict.fromkeys(mcp_tools_named_in_text(prompt_text))
        if name in available
    ]
    if not requested:
        return []

    args = {}
    if "empty arguments" in prompt_text.lower():
        args = {}

    oma_events: list[dict[str, Any]] = []
    for tool_name in requested:
        oma_events.extend(
            await _run_tool_call(
                session,
                tool_name=tool_name,
                args=args,
                fallback_tools=mcp_runtime.tools,
            )
        )
    return oma_events


def _tool_result_text(result: Any) -> str:
    if result is None:
        return ""
    content = getattr(result, "content", None)
    if isinstance(content, list):
        parts: list[str] = []
        for block in content:
            text = getattr(block, "text", None)
            if text is None and isinstance(block, dict):
                text = block.get("text")
            if text:
                parts.append(str(text))
        joined = "".join(parts).strip()
        if joined:
            return joined
    if isinstance(result, str):
        return result
    return str(result)
