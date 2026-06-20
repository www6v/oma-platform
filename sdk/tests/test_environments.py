"""E2E tests for /v1/environments."""

from __future__ import annotations

import anthropic

from oma_sdk.examples import EnvironmentExamples


def test_environments_create_and_retrieve(client: anthropic.Anthropic):
    result = EnvironmentExamples.create_and_retrieve(client)
    assert result["environment"].id
    assert result["retrieved"].id == result["environment"].id
    assert result["retrieved"].name == result["environment"].name


def test_environments_list(client: anthropic.Anthropic):
    envs = EnvironmentExamples.list_environments(client)
    assert isinstance(envs, list)
    assert len(envs) >= 1  # at least the default env exists


def test_environments_update(client: anthropic.Anthropic):
    result = EnvironmentExamples.update_environment(client)
    assert result["updated"].name == "sdk-e2e-env-after"


def test_environments_archive(client: anthropic.Anthropic):
    result = EnvironmentExamples.archive_environment(client)
    assert result["archived"].id == result["environment"].id
