"""E2E tests for oma-platform-only endpoints:
  /v1/me, /v1/models/list, /v1/api_keys,
  /v1/integrations/{provider}/installations, /v1/dreams,
  /v1/cost_report, /v1/files
"""

from __future__ import annotations

import anthropic

from oma_sdk.examples import MiscExamples


def test_me(client: anthropic.Anthropic):
    data = MiscExamples.get_me(client)
    assert "user" in data
    assert "id" in data["user"]
    assert "email" in data["user"]


def test_models_list(client: anthropic.Anthropic):
    data = MiscExamples.list_models(client)
    assert "data" in data


def test_api_keys_list(client: anthropic.Anthropic):
    data = MiscExamples.list_api_keys(client)
    assert "data" in data


def test_api_keys_create_and_delete(client: anthropic.Anthropic):
    data = MiscExamples.create_and_delete_api_key(client)
    assert "id" in data


def test_integrations_github_installations(client: anthropic.Anthropic):
    data = MiscExamples.list_github_installations(client)
    assert "data" in data


def test_integrations_linear_installations(client: anthropic.Anthropic):
    data = MiscExamples.list_linear_installations(client)
    assert "data" in data


def test_dreams_list(client: anthropic.Anthropic):
    data = MiscExamples.list_dreams(client)
    assert "data" in data
    assert isinstance(data["data"], list)


def test_cost_report(client: anthropic.Anthropic):
    data = MiscExamples.get_cost_report(client)
    assert "type" in data
    assert data["type"] == "cost_report"
    assert "usage" in data


def test_files_list(client: anthropic.Anthropic):
    files = MiscExamples.list_files(client)
    assert isinstance(files, list)
