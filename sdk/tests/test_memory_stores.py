"""E2E tests for /v1/memory_stores — CRUD + memories + memory_versions."""

from __future__ import annotations

import anthropic


def test_memory_stores_create_and_retrieve(client: anthropic.Anthropic):
    ms = client.beta.memory_stores.create(name="sdk-e2e-ms-create")
    try:
        assert ms.id
        got = client.beta.memory_stores.retrieve(ms.id)
        assert got.id == ms.id
    finally:
        client.beta.memory_stores.archive(ms.id)


def test_memory_stores_list(client: anthropic.Anthropic):
    page = client.beta.memory_stores.list()
    assert isinstance(list(page), list)


def test_memory_stores_update(client: anthropic.Anthropic):
    ms = client.beta.memory_stores.create(name="sdk-e2e-ms-before")
    try:
        updated = client.beta.memory_stores.update(ms.id, name="sdk-e2e-ms-after")
        assert updated.name == "sdk-e2e-ms-after"
    finally:
        client.beta.memory_stores.archive(ms.id)


def test_memory_stores_memories_create_and_list(client: anthropic.Anthropic):
    ms = client.beta.memory_stores.create(name="sdk-e2e-ms-memories")
    try:
        mem = client.beta.memory_stores.memories.create(
            ms.id,
            content="sdk e2e test memory content",
            path="/sdk/e2e/test",
        )
        assert mem.id

        page = client.beta.memory_stores.memories.list(ms.id)
        mems = list(page)
        assert any(m.id == mem.id for m in mems)
    finally:
        client.beta.memory_stores.archive(ms.id)


def test_memory_stores_memory_versions(client: anthropic.Anthropic):
    ms = client.beta.memory_stores.create(name="sdk-e2e-ms-versions")
    try:
        page = client.beta.memory_stores.memory_versions.list(ms.id)
        assert isinstance(list(page), list)
    finally:
        client.beta.memory_stores.archive(ms.id)


def test_memory_stores_archive(client: anthropic.Anthropic):
    ms = client.beta.memory_stores.create(name="sdk-e2e-ms-archive")
    archived = client.beta.memory_stores.archive(ms.id)
    assert archived.id == ms.id
