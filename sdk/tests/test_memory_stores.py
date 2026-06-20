"""E2E tests for /v1/memory_stores — CRUD + memories + memory_versions."""

from __future__ import annotations

import anthropic

from oma_sdk.examples import MemoryStoreExamples


def test_memory_stores_create_and_retrieve(client: anthropic.Anthropic):
    result = MemoryStoreExamples.create_and_retrieve(client)
    assert result["memory_store"].id
    assert result["retrieved"].id == result["memory_store"].id


def test_memory_stores_list(client: anthropic.Anthropic):
    memory_stores = MemoryStoreExamples.list_memory_stores(client)
    assert isinstance(memory_stores, list)


def test_memory_stores_update(client: anthropic.Anthropic):
    result = MemoryStoreExamples.update_memory_store(client)
    assert result["updated"].name == "sdk-e2e-ms-after"


def test_memory_stores_memories_create_and_list(client: anthropic.Anthropic):
    result = MemoryStoreExamples.create_and_list_memories(client)
    assert result["memory"].id
    assert any(m.id == result["memory"].id for m in result["memories"])


def test_memory_stores_memory_versions(client: anthropic.Anthropic):
    result = MemoryStoreExamples.list_memory_versions(client)
    assert isinstance(result["versions"], list)


def test_memory_stores_archive(client: anthropic.Anthropic):
    result = MemoryStoreExamples.archive_memory_store(client)
    assert result["archived"].id == result["memory_store"].id
