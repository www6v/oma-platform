"""E2E tests for /v1/agents."""

from __future__ import annotations

import anthropic

MODEL = {"id": "claude-sonnet-4-6"}


def test_agents_create_and_retrieve(client: anthropic.Anthropic):
    agent = client.beta.agents.create(name="sdk-e2e-create", model=MODEL)
    try:
        assert agent.id
        assert agent.name == "sdk-e2e-create"

        got = client.beta.agents.retrieve(agent.id)
        assert got.id == agent.id
        assert got.name == agent.name
    finally:
        client.beta.agents.archive(agent.id)


def test_agents_list(client: anthropic.Anthropic):
    page = client.beta.agents.list()
    agents = list(page)
    assert isinstance(agents, list)


def test_agents_update(client: anthropic.Anthropic):
    agent = client.beta.agents.create(name="sdk-e2e-update-before", model=MODEL)
    try:
        updated = client.beta.agents.update(agent.id, version=1, name="sdk-e2e-update-after")
        assert updated.name == "sdk-e2e-update-after"
    finally:
        client.beta.agents.archive(agent.id)


def test_agents_versions(client: anthropic.Anthropic):
    agent = client.beta.agents.create(name="sdk-e2e-versions", model=MODEL)
    try:
        page = client.beta.agents.versions.list(agent.id)
        versions = list(page)
        assert isinstance(versions, list)
    finally:
        client.beta.agents.archive(agent.id)


def test_agents_archive(client: anthropic.Anthropic):
    agent = client.beta.agents.create(name="sdk-e2e-archive", model=MODEL)
    archived = client.beta.agents.archive(agent.id)
    assert archived.id == agent.id
