"""
E2E test fixtures for oma-sdk.

All tests hit a real oma-platform server. Set OMA_BASE_URL and OMA_API_KEY, or
place them in oma-platform/.env (auto-loaded).

  OMA_API_KEY=dev-key OMA_BASE_URL=http://124.221.28.203:8787 pytest tests/
"""

from __future__ import annotations

import os
from pathlib import Path

import pytest

from oma_sdk import OMAClient

# Auto-load oma-platform/.env (two levels up from sdk/tests/)
_dotenv = Path(__file__).parents[2] / ".env"
if _dotenv.exists():
    for _line in _dotenv.read_text().splitlines():
        _line = _line.strip()
        if not _line or _line.startswith("#") or "=" not in _line:
            continue
        _k, _v = _line.split("=", 1)
        os.environ.setdefault(_k.strip(), _v.strip())


def base_url() -> str:
    return os.getenv("OMA_BASE_URL", "http://localhost:8787")


@pytest.fixture
async def client():
    async with OMAClient(base_url=base_url()) as c:
        yield c


@pytest.fixture
async def tmp_env(client: OMAClient):
    """Creates a throw-away environment; archives it after the test."""
    env = client.environments.create(name="sdk-e2e-tmp-env")
    yield env.id
    try:
        client.environments.archive(env.id)
    except Exception:
        pass


@pytest.fixture
async def tmp_agent(client: OMAClient):
    """Creates a throw-away agent; archives it after the test."""
    agent = client.agents.create(
        name="sdk-e2e-tmp-agent",
        model={"id": "claude-sonnet-4-6"},
        system="SDK e2e test agent. Do not use in production.",
    )
    yield agent
    try:
        client.agents.archive(agent.id)
    except Exception:
        pass
