"""Tests for platform exec bash adapter (T28 remote sandbox)."""

from __future__ import annotations

import pytest
import respx
from httpx import Response

from oma_adapter.outbound.platform_exec_bash import PlatformExecBashOperations


@pytest.mark.asyncio
@respx.mock
async def test_platform_exec_posts_to_session_exec() -> None:
    route = respx.post("http://127.0.0.1:8080/v1/sessions/sess-1/exec").mock(
        return_value=Response(200, json={"output": "done\n"}),
    )
    ops = PlatformExecBashOperations(
        session_id="sess-1",
        platform_base="http://127.0.0.1:8080",
        api_key="dev-key",
    )
    chunks: list[bytes] = []

    def on_data(data: bytes) -> None:
        chunks.append(data)

    code = await ops.exec(
        "echo done",
        "/workspace",
        on_data=on_data,
        signal=None,
        timeout=30.0,
    )
    assert code == 0
    assert b"done" in b"".join(chunks)
    assert route.called
    assert route.calls[0].request.headers["x-api-key"] == "dev-key"


@pytest.mark.asyncio
@respx.mock
async def test_platform_exec_propagates_http_error() -> None:
    respx.post("http://127.0.0.1:8080/v1/sessions/sess-2/exec").mock(
        return_value=Response(500, text="boom"),
    )
    ops = PlatformExecBashOperations(
        session_id="sess-2",
        platform_base="http://127.0.0.1:8080",
    )
    code = await ops.exec(
        "false",
        "/workspace",
        on_data=None,
        signal=None,
        timeout=5.0,
    )
    assert code == 1
