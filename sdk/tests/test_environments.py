"""E2E tests for /v1/environments."""

from __future__ import annotations

import anthropic


def test_environments_create_and_retrieve(client: anthropic.Anthropic):
    env = client.beta.environments.create(name="sdk-e2e-env-create")
    try:
        assert env.id
        got = client.beta.environments.retrieve(env.id)
        assert got.id == env.id
        assert got.name == env.name
    finally:
        client.beta.environments.archive(env.id)


def test_environments_list(client: anthropic.Anthropic):
    page = client.beta.environments.list()
    envs = list(page)
    assert isinstance(envs, list)
    assert len(envs) >= 1  # at least the default env exists


def test_environments_update(client: anthropic.Anthropic):
    env = client.beta.environments.create(name="sdk-e2e-env-before")
    try:
        updated = client.beta.environments.update(env.id, name="sdk-e2e-env-after")
        assert updated.name == "sdk-e2e-env-after"
    finally:
        client.beta.environments.archive(env.id)


def test_environments_archive(client: anthropic.Anthropic):
    env = client.beta.environments.create(name="sdk-e2e-env-archive")
    archived = client.beta.environments.archive(env.id)
    assert archived.id == env.id
