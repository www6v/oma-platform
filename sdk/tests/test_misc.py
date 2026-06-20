"""E2E tests for oma-platform-only endpoints:
  /v1/me, /v1/models/list, /v1/api_keys,
  /v1/integrations/{provider}/installations, /v1/dreams,
  /v1/cost_report, /v1/files
"""

from __future__ import annotations

import anthropic
import httpx

_DREAMS_BETA = "managed-agents-2026-04-01,dreaming-2026-04-21"


def test_me(client: anthropic.Anthropic):
    resp = client.get("/v1/me", cast_to=httpx.Response)
    data = resp.json()
    assert "user" in data
    assert "id" in data["user"]
    assert "email" in data["user"]


def test_models_list(client: anthropic.Anthropic):
    resp = client.get("/v1/models/list", cast_to=httpx.Response)
    data = resp.json()
    assert "data" in data
    ids = [m["id"] for m in data["data"]]
    assert any("claude" in i for i in ids)


def test_api_keys_list(client: anthropic.Anthropic):
    resp = client.get("/v1/api_keys", cast_to=httpx.Response)
    data = resp.json()
    assert "data" in data


def test_api_keys_create_and_delete(client: anthropic.Anthropic):
    resp = client.post("/v1/api_keys", cast_to=httpx.Response, body={"name": "sdk-e2e-key"})
    data = resp.json()
    assert "id" in data
    key_id = data["id"]
    client.delete(f"/v1/api_keys/{key_id}", cast_to=httpx.Response)


def test_integrations_github_installations(client: anthropic.Anthropic):
    resp = client.get("/v1/integrations/github/installations", cast_to=httpx.Response)
    data = resp.json()
    assert "data" in data


def test_integrations_linear_installations(client: anthropic.Anthropic):
    resp = client.get("/v1/integrations/linear/installations", cast_to=httpx.Response)
    data = resp.json()
    assert "data" in data


def test_dreams_list(client: anthropic.Anthropic):
    resp = client.get("/v1/dreams", cast_to=httpx.Response)
    data = resp.json()
    assert "data" in data
    assert isinstance(data["data"], list)


def test_cost_report(client: anthropic.Anthropic):
    resp = client.get("/v1/cost_report", cast_to=httpx.Response)
    data = resp.json()
    assert "type" in data
    assert data["type"] == "cost_report"
    assert "usage" in data


def test_files_list(client: anthropic.Anthropic):
    page = client.beta.files.list()
    assert isinstance(list(page), list)
