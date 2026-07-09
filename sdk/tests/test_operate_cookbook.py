"""Operate in production cookbook tests."""

from __future__ import annotations

import os
import sys
from pathlib import Path

import pytest

_EXAMPLE10 = Path(__file__).resolve().parents[1] / "example" / "example10"
if str(_EXAMPLE10) not in sys.path:
    sys.path.insert(0, str(_EXAMPLE10))

from operate_fixtures import (  # noqa: E402
    AGENT_NAME,
    ENV_CONFIG,
    GITHUB_MCP_SERVER_NAME,
)

_RUN_LIVE = os.getenv("OMA_RUN_LIVE_OPERATE", "0") == "1"


def test_operate_fixtures_constants() -> None:
    assert AGENT_NAME == "operate-production"
    assert GITHUB_MCP_SERVER_NAME == "github"
    assert ENV_CONFIG["networking"]["type"] == "limited"


@pytest.mark.live
@pytest.mark.asyncio
@pytest.mark.skipif(
    not _RUN_LIVE,
    reason="set OMA_RUN_LIVE_OPERATE=1 for live operate soak",
)
async def test_operate_in_production_live_soak() -> None:
    from oma_sdk import OMAClient
    from operate_in_production_soak import run_operate_in_production_soak

    token = os.getenv("GITHUB_TOKEN", "")
    if not token:
        pytest.skip("GITHUB_TOKEN required")

    mcp_url = os.getenv(
        "GITHUB_MCP_URL",
        "https://api.githubcopilot.com/mcp/",
    )
    base = os.getenv("OMA_BASE_URL", "http://localhost:8787")
    model = os.getenv("OMA_MODEL", "qwen3.7-plus")

    async with OMAClient(base_url=base) as client:
        result = await run_operate_in_production_soak(
            client,
            model=model,
            mcp_server_url=mcp_url,
            github_token=token,
        )
    assert result["vault_ids"]
    assert result["session_id"]
