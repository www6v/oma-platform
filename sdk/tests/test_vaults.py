"""E2E tests for /v1/vaults — CRUD + credentials."""

from __future__ import annotations

from oma_sdk import OMAClient


async def test_vaults_create_and_retrieve(client: OMAClient):
    # extra_body={"name": ...} bridges oma-platform until display_name alias is deployed
    vault = client.vaults.create(
        display_name="sdk-e2e-vault-create",
        extra_body={"name": "sdk-e2e-vault-create"},
    )
    try:
        assert vault.id
        got = client.vaults.retrieve(vault.id)
        assert got.id == vault.id
    finally:
        client.vaults.archive(vault.id)


async def test_vaults_list(client: OMAClient):
    page = client.vaults.list()
    assert isinstance(list(page), list)


async def test_vaults_credentials_list(client: OMAClient):
    vault = client.vaults.create(
        display_name="sdk-e2e-vault-creds",
        extra_body={"name": "sdk-e2e-vault-creds"},
    )
    try:
        page = client.vaults.credentials.list(vault.id)
        assert isinstance(list(page), list)
    finally:
        client.vaults.archive(vault.id)


async def test_vaults_archive(client: OMAClient):
    vault = client.vaults.create(
        display_name="sdk-e2e-vault-archive",
        extra_body={"name": "sdk-e2e-vault-archive"},
    )
    archived = client.vaults.archive(vault.id)
    assert archived.id == vault.id
