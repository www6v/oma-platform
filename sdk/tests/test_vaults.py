"""E2E tests for /v1/vaults — CRUD + credentials."""

from __future__ import annotations

import os

import anthropic

_KEEP = os.getenv("OMA_KEEP_RESOURCES", "0") == "1"


def test_vaults_create_and_retrieve(client: anthropic.Anthropic):
    vault = client.beta.vaults.create(
        display_name="sdk-e2e-vault-create",
        extra_body={"name": "sdk-e2e-vault-create"},
    )
    try:
        assert vault.id
        got = client.beta.vaults.retrieve(vault.id)
        assert got.id == vault.id
    finally:
        if not _KEEP:
            client.beta.vaults.archive(vault.id)
        else:
            print(f"\n[KEEP] vault {vault.id} (sdk-e2e-vault-create) — archive manually when done")


def test_vaults_list(client: anthropic.Anthropic):
    page = client.beta.vaults.list()
    assert isinstance(list(page), list)


def test_vaults_credentials_list(client: anthropic.Anthropic):
    vault = client.beta.vaults.create(
        display_name="sdk-e2e-vault-creds",
        extra_body={"name": "sdk-e2e-vault-creds"},
    )
    try:
        page = client.beta.vaults.credentials.list(vault.id)
        assert isinstance(list(page), list)
    finally:
        if not _KEEP:
            client.beta.vaults.archive(vault.id)
        else:
            print(f"\n[KEEP] vault {vault.id} (sdk-e2e-vault-creds) — archive manually when done")


def test_vaults_archive(client: anthropic.Anthropic):
    vault = client.beta.vaults.create(
        display_name="sdk-e2e-vault-archive",
        extra_body={"name": "sdk-e2e-vault-archive"},
    )
    archived = client.beta.vaults.archive(vault.id)
    assert archived.id == vault.id
