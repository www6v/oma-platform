"""E2E tests for /v1/vaults — CRUD + credentials."""

from __future__ import annotations

import anthropic

from oma_sdk.examples import VaultExamples


def test_vaults_create_and_retrieve(client: anthropic.Anthropic):
    result = VaultExamples.create_and_retrieve(client)
    assert result["vault"].id
    assert result["retrieved"].id == result["vault"].id


def test_vaults_list(client: anthropic.Anthropic):
    vaults = VaultExamples.list_vaults(client)
    assert isinstance(vaults, list)


def test_vaults_credentials_list(client: anthropic.Anthropic):
    result = VaultExamples.list_credentials(client)
    assert isinstance(result["credentials"], list)


def test_vaults_archive(client: anthropic.Anthropic):
    result = VaultExamples.archive_vault(client)
    assert result["archived"].id == result["vault"].id
