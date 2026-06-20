"""E2E tests for /v1/agents."""

from __future__ import annotations

import anthropic

from oma_sdk.examples import AgentExamples


def test_agents_create_and_retrieve(client: anthropic.Anthropic):
    result = AgentExamples.create_and_retrieve(client)
    assert result["agent"].id
    assert result["agent"].name == "sdk-e2e-create"
    assert result["retrieved"].id == result["agent"].id


def test_agents_list(client: anthropic.Anthropic):
    agents = AgentExamples.list_agents(client)
    assert isinstance(agents, list)


def test_agents_update(client: anthropic.Anthropic):
    result = AgentExamples.update_agent(client)
    assert result["updated"].name == "sdk-e2e-update-after"


def test_agents_versions(client: anthropic.Anthropic):
    result = AgentExamples.list_agent_versions(client)
    assert isinstance(result["versions"], list)


def test_agents_archive(client: anthropic.Anthropic):
    result = AgentExamples.archive_agent(client)
    assert result["archived"].id == result["agent"].id
