import pytest

from oma_adapter.mcp.client import McpProxyClient, _format_tool_result
from oma_adapter.mcp.setup import discover_mcp_tools


@pytest.mark.asyncio
async def test_mcp_proxy_client_list_and_call(httpx_mock) -> None:
    base = "http://oma.test/v1/mcp-proxy/sess-1/demo"
    httpx_mock.add_response(
        method="POST",
        url=base,
        json={
            "jsonrpc": "2.0",
            "id": 1,
            "result": {
                "protocolVersion": "2024-11-05",
                "capabilities": {},
                "serverInfo": {"name": "demo", "version": "1.0"},
            },
        },
    )
    httpx_mock.add_response(
        method="POST",
        url=base,
        json={"jsonrpc": "2.0", "id": 2, "result": {}},
    )
    httpx_mock.add_response(
        method="POST",
        url=base,
        json={
            "jsonrpc": "2.0",
            "id": 3,
            "result": {
                "tools": [
                    {
                        "name": "ping",
                        "description": "Ping",
                        "inputSchema": {
                            "type": "object",
                            "properties": {},
                        },
                    }
                ]
            },
        },
    )
    httpx_mock.add_response(
        method="POST",
        url=base,
        json={
            "jsonrpc": "2.0",
            "id": 4,
            "result": {
                "content": [{"type": "text", "text": "pong"}],
            },
        },
    )

    client = McpProxyClient(proxy_url=base, api_key="test-key")
    try:
        tools = await client.list_tools()
        assert len(tools) == 1
        assert tools[0]["name"] == "ping"
        text = await client.call_tool("ping", {})
        assert text == "pong"
    finally:
        await client.aclose()


def test_format_tool_result_text_blocks() -> None:
    out = _format_tool_result(
        {"content": [{"type": "text", "text": "hello"}]},
    )
    assert out == "hello"


@pytest.mark.asyncio
async def test_discover_mcp_tools_registers_harness_names(httpx_mock) -> None:
    proxy = (
        "http://127.0.0.1:8787/v1/mcp-proxy/sess-abc/linear"
    )
    for response in (
        {
            "jsonrpc": "2.0",
            "id": 1,
            "result": {
                "protocolVersion": "2024-11-05",
                "capabilities": {},
                "serverInfo": {"name": "linear", "version": "1"},
            },
        },
        {"jsonrpc": "2.0", "id": 2, "result": {}},
        {
            "jsonrpc": "2.0",
            "id": 3,
            "result": {
                "tools": [
                    {
                        "name": "search_issues",
                        "description": "Search issues",
                        "inputSchema": {
                            "type": "object",
                            "properties": {"q": {"type": "string"}},
                        },
                    }
                ]
            },
        },
    ):
        httpx_mock.add_response(method="POST", url=proxy, json=response)

    runtime = await discover_mcp_tools(
        mcp_servers=[
            {
                "name": "linear",
                "type": "url",
                "url": "https://mcp.linear.app/mcp",
            }
        ],
        session_id="sess-abc",
        proxy_base="http://127.0.0.1:8787",
        proxy_api_key="omak_test",
    )
    assert len(runtime.tools) == 1
    assert runtime.tools[0].name == "mcp__linear__search_issues"
