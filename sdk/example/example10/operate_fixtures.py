"""Fixtures for CMA_operate_in_production cookbook example."""

from __future__ import annotations

AGENT_NAME = "operate-production"
AGENT_SYSTEM = (
    "You operate in production. Use the github MCP toolset to inspect repositories."
)
SESSION_TITLE = "Operate in production"
ENV_NAME = "operate-local"

ENV_CONFIG = {
    "name": ENV_NAME,
    "networking": {"type": "limited"},
}

GITHUB_MCP_SERVER_NAME = "github"
