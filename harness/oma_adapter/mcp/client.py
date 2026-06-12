"""JSON-RPC MCP client routed through OMA /v1/mcp-proxy."""

from __future__ import annotations

import json
from typing import Any

import httpx

MCP_PROTOCOL_VERSION = "2024-11-05"
MCP_SETUP_TIMEOUT_SEC = 15.0


class McpProxyClient:
    """Minimal streamable-HTTP MCP client via the OMA credential proxy."""

    def __init__(
        self,
        *,
        proxy_url: str,
        api_key: str | None,
    ) -> None:
        self._proxy_url = proxy_url.rstrip("/")
        self._headers = {
            "Accept": "application/json, text/event-stream",
            "Content-Type": "application/json",
        }
        if api_key:
            self._headers["Authorization"] = f"Bearer {api_key}"
        self._client = httpx.AsyncClient(timeout=MCP_SETUP_TIMEOUT_SEC)
        self._req_id = 0
        self._initialized = False

    async def aclose(self) -> None:
        await self._client.aclose()

    async def _rpc(self, method: str, params: dict[str, Any] | None = None) -> Any:
        self._req_id += 1
        payload = {
            "jsonrpc": "2.0",
            "id": self._req_id,
            "method": method,
            "params": params or {},
        }
        resp = await self._client.post(
            self._proxy_url,
            json=payload,
            headers=self._headers,
        )
        resp.raise_for_status()
        data = _parse_mcp_response(resp)
        if isinstance(data, dict) and data.get("error"):
            err = data["error"]
            message = err.get("message", str(err))
            raise RuntimeError(message)
        if isinstance(data, dict):
            return data.get("result")
        return data

    async def initialize(self) -> None:
        await self._rpc(
            "initialize",
            {
                "protocolVersion": MCP_PROTOCOL_VERSION,
                "capabilities": {},
                "clientInfo": {"name": "oma-harness", "version": "0.1.0"},
            },
        )
        # Notification — no response expected on some servers; best-effort.
        try:
            await self._rpc("notifications/initialized", {})
        except Exception:  # noqa: BLE001 — optional on minimal MCP servers
            pass
        self._initialized = True

    async def list_tools(self) -> list[dict[str, Any]]:
        if not self._initialized:
            await self.initialize()
        result = await self._rpc("tools/list")
        if not isinstance(result, dict):
            return []
        tools = result.get("tools")
        if not isinstance(tools, list):
            return []
        return [t for t in tools if isinstance(t, dict)]

    async def call_tool(self, name: str, arguments: dict[str, Any]) -> str:
        if not self._initialized:
            await self.initialize()
        result = await self._rpc(
            "tools/call",
            {"name": name, "arguments": arguments},
        )
        return _format_tool_result(result)


def _parse_mcp_response(resp: httpx.Response) -> Any:
    content_type = resp.headers.get("content-type", "")
    if "application/json" in content_type:
        return resp.json()
    text = resp.text.strip()
    if not text:
        return {}
    # Streamable HTTP may return SSE — take the last data: line.
    if text.startswith("event:") or text.startswith("data:"):
        for line in reversed(text.splitlines()):
            if line.startswith("data:"):
                body = line[5:].strip()
                if body:
                    return json.loads(body)
        return {}
    return json.loads(text)


def _format_tool_result(result: Any) -> str:
    if not isinstance(result, dict):
        return json.dumps(result, ensure_ascii=False)
    content = result.get("content")
    if not isinstance(content, list):
        return json.dumps(result, ensure_ascii=False)
    parts: list[str] = []
    for block in content:
        if not isinstance(block, dict):
            continue
        if block.get("type") == "text" and isinstance(block.get("text"), str):
            parts.append(block["text"])
        else:
            parts.append(json.dumps(block, ensure_ascii=False))
    if parts:
        return "\n".join(parts)
    if result.get("isError"):
        return json.dumps(result, ensure_ascii=False)
    return json.dumps(result, ensure_ascii=False)
