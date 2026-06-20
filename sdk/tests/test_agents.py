"""E2E tests for /v1/agents."""

from __future__ import annotations

import pytest
from oma_sdk import OMAClient

MODEL = {"id": "claude-sonnet-4-6"}


async def test_agents_create_and_retrieve(client: OMAClient):
    agent = client.agents.create(name="sdk-e2e-create", model=MODEL)
    try:
        assert agent.id
        assert agent.name == "sdk-e2e-create"

        got = client.agents.retrieve(agent.id)
        assert got.id == agent.id
        assert got.name == agent.name
    finally:
        client.agents.archive(agent.id)


async def test_agents_list(client: OMAClient):
    page = client.agents.list()
    agents = list(page)
    assert isinstance(agents, list)


async def test_agents_update(client: OMAClient):
    agent = client.agents.create(name="sdk-e2e-update-before", model=MODEL)
    try:
        updated = client.agents.update(agent.id, version=1, name="sdk-e2e-update-after")
        assert updated.name == "sdk-e2e-update-after"
    finally:
        client.agents.archive(agent.id)


async def test_agents_versions(client: OMAClient):
    agent = client.agents.create(name="sdk-e2e-versions", model=MODEL)
    try:
        page = client.agents.versions.list(agent.id)
        versions = list(page)
        assert isinstance(versions, list)
    finally:
        client.agents.archive(agent.id)


async def test_agents_archive(client: OMAClient):
    agent = client.agents.create(name="sdk-e2e-archive", model=MODEL)
    archived = client.agents.archive(agent.id)
    assert archived.id == agent.id
