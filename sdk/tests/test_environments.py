"""E2E tests for /v1/environments."""

from __future__ import annotations

from oma_sdk import OMAClient


async def test_environments_create_and_retrieve(client: OMAClient):
    env = client.environments.create(name="sdk-e2e-env-create")
    try:
        assert env.id
        got = client.environments.retrieve(env.id)
        assert got.id == env.id
        assert got.name == env.name
    finally:
        client.environments.archive(env.id)


async def test_environments_list(client: OMAClient):
    page = client.environments.list()
    envs = list(page)
    assert isinstance(envs, list)
    assert len(envs) >= 1  # at least the default env exists


async def test_environments_update(client: OMAClient):
    env = client.environments.create(name="sdk-e2e-env-before")
    try:
        updated = client.environments.update(env.id, name="sdk-e2e-env-after")
        assert updated.name == "sdk-e2e-env-after"
    finally:
        client.environments.archive(env.id)


async def test_environments_archive(client: OMAClient):
    env = client.environments.create(name="sdk-e2e-env-archive")
    archived = client.environments.archive(env.id)
    assert archived.id == env.id
