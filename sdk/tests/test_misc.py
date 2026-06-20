"""E2E tests for oma-platform-only endpoints:
  /v1/me, /v1/models/list, /v1/api_keys,
  /v1/integrations/{provider}/, /v1/dreams,
  /v1/cost_report, /v1/files
"""

from __future__ import annotations

import pytest
from oma_sdk import OMAClient


async def test_me(client: OMAClient):
    resp = await client.me.get()
    assert "user" in resp
    assert "id" in resp["user"]
    assert "email" in resp["user"]


async def test_models_list(client: OMAClient):
    resp = await client.models.list()
    assert "data" in resp
    ids = [m["id"] for m in resp["data"]]
    assert any("claude" in i for i in ids)


async def test_api_keys_list(client: OMAClient):
    resp = await client.api_keys.list()
    assert "data" in resp


async def test_api_keys_create_and_delete(client: OMAClient):
    resp = await client.api_keys.create(name="sdk-e2e-key")
    assert "id" in resp
    key_id = resp["id"]
    await client.api_keys.delete(key_id)


async def test_integrations_github_installations(client: OMAClient):
    resp = await client.integrations.list_installations("github")
    assert "data" in resp


async def test_integrations_linear_installations(client: OMAClient):
    resp = await client.integrations.list_installations("linear")
    assert "data" in resp


async def test_dreams_list(client: OMAClient):
    resp = await client.dreams.list()
    assert "data" in resp
    assert isinstance(resp["data"], list)


async def test_cost_report(client: OMAClient):
    resp = await client.cost_report.get()
    assert "type" in resp
    assert resp["type"] == "cost_report"
    assert "usage" in resp


async def test_files_list(client: OMAClient):
    resp = await client.files.list()
    assert "data" in resp
