"""Discover MCP tools and wrap them as piPy AgentTool instances."""

from __future__ import annotations

import json
import logging
from typing import Any

from pi_agent.types import AgentToolResult
from pi_ai.types import TextContent

from oma_adapter.mcp.client import McpProxyClient
from oma_adapter.mcp.runtime import McpRuntime, configure_mcp_runtime

logger = logging.getLogger(__name__)

MCP_TOOL_PREFIX = "mcp__"


def _parse_mcp_servers(raw: list[dict[str, Any]] | None) -> list[dict[str, Any]]:
    if not raw:
        return []
    out: list[dict[str, Any]] = []
    for entry in raw:
        if not isinstance(entry, dict):
            continue
        name = entry.get("name")
        url = entry.get("url")
        if isinstance(name, str) and name and isinstance(url, str) and url:
            out.append(entry)
    return out


def _proxy_url(base: str, session_id: str, server_name: str) -> str:
    return f"{base.rstrip('/')}/v1/mcp-proxy/{session_id}/{server_name}"


def _make_tool_class(
    *,
    tool_name: str,
    description: str,
    parameters: dict[str, Any],
    call_fn: Any,
) -> type:
    class McpAgentTool:
        name = tool_name
        execution_mode = "parallel"

        def __init__(self) -> None:
            self.description = description
            self.parameters = parameters

        async def execute(
            self,
            tool_call_id: str,
            args: dict[str, Any],
            signal: Any = None,
            on_update: Any = None,
        ) -> AgentToolResult:
            del tool_call_id, signal, on_update
            try:
                text = await call_fn(args)
            except Exception as exc:  # noqa: BLE001
                return AgentToolResult(
                    content=[TextContent(text=f"Error: {exc}")],
                    is_error=True,
                )
            is_error = text.startswith("Error:")
            return AgentToolResult(
                content=[TextContent(text=text)],
                is_error=is_error,
            )

    return McpAgentTool


async def discover_mcp_tools(
    *,
    mcp_servers: list[dict[str, Any]] | None,
    session_id: str,
    proxy_base: str | None,
    proxy_api_key: str | None,
) -> McpRuntime:
    servers = _parse_mcp_servers(mcp_servers)
    if not servers or not proxy_base:
        return McpRuntime(tools=[])

    tools: list[Any] = []
    for server in servers:
        server_name = str(server["name"])
        proxy_url = _proxy_url(proxy_base, session_id, server_name)
        client = McpProxyClient(proxy_url=proxy_url, api_key=proxy_api_key)
        try:
            remote = await client.list_tools()
        except Exception as exc:  # noqa: BLE001
            logger.warning(
                "MCP setup failed for %s (%s): %s",
                server_name,
                server.get("url"),
                exc,
            )
            await client.aclose()
            continue

        for tool_def in remote:
            remote_name = tool_def.get("name")
            if not isinstance(remote_name, str) or not remote_name:
                continue
            harness_name = f"{MCP_TOOL_PREFIX}{server_name}__{remote_name}"
            schema = tool_def.get("inputSchema")
            if not isinstance(schema, dict):
                schema = {"type": "object", "properties": {}}
            description = tool_def.get("description") or f"MCP tool {remote_name}"

            async def _call(
                args: dict[str, Any],
                *,
                _client: McpProxyClient = client,
                _remote_name: str = remote_name,
            ) -> str:
                return await _client.call_tool(_remote_name, args)

            tool_cls = _make_tool_class(
                tool_name=harness_name,
                description=str(description),
                parameters=schema,
                call_fn=_call,
            )
            tools.append(tool_cls())

        # Keep client alive for turn duration via closure references.
        if not remote:
            await client.aclose()

    return McpRuntime(tools=tools)


async def setup_mcp_runtime_for_turn(
    *,
    mcp_servers: list[dict[str, Any]] | None,
    session_id: str,
    proxy_base: str | None,
    proxy_api_key: str | None,
) -> None:
    runtime = await discover_mcp_tools(
        mcp_servers=mcp_servers,
        session_id=session_id,
        proxy_base=proxy_base,
        proxy_api_key=proxy_api_key,
    )
    configure_mcp_runtime(runtime if runtime.tools else None)


def mcp_servers_from_agent(agent: Any) -> list[dict[str, Any]] | None:
    raw = getattr(agent, "mcp_servers", None)
    if raw is None:
        return None
    if isinstance(raw, str):
        try:
            parsed = json.loads(raw)
        except json.JSONDecodeError:
            return None
        raw = parsed
    if isinstance(raw, list):
        return raw
    return None
