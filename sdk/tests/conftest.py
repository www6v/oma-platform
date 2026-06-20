"""
E2E test fixtures for oma-sdk.

All tests hit a real oma-platform server using the anthropic Python SDK.
Set OMA_BASE_URL and OMA_API_KEY, or place them in oma-platform/.env (auto-loaded).

  OMA_API_KEY=dev-key OMA_BASE_URL=http://124.221.28.203:8787 pytest tests/
"""

from __future__ import annotations

import os
from pathlib import Path

import anthropic
import pytest

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
def client() -> anthropic.Anthropic:
    return anthropic.Anthropic(
        api_key=os.environ["OMA_API_KEY"],
        base_url=base_url(),
        default_headers={"anthropic-beta": "managed-agents-2026-04-01,dreaming-2026-04-21"},
    )


@pytest.fixture
def tmp_env(client: anthropic.Anthropic):
    """Creates a throw-away environment; archives it after the test."""
    env = client.beta.environments.create(name="sdk-e2e-tmp-env")
    yield env.id
    try:
        client.beta.environments.archive(env.id)
    except Exception:
        pass


@pytest.fixture
def tmp_agent(client: anthropic.Anthropic):
    """Creates a throw-away agent; archives it after the test."""
    agent = client.beta.agents.create(
        name="sdk-e2e-tmp-agent",
        model={"id": "claude-sonnet-4-6"},
        system="SDK e2e test agent. Do not use in production.",
    )
    yield agent
    try:
        client.beta.agents.archive(agent.id)
    except Exception:
        pass
