"""E2E tests for /v1/memory_stores — CRUD + memories + memory_versions."""

from __future__ import annotations

from oma_sdk import OMAClient


async def test_memory_stores_create_and_retrieve(client: OMAClient):
    ms = client.memory_stores.create(name="sdk-e2e-ms-create")
    try:
        assert ms.id
        got = client.memory_stores.retrieve(ms.id)
        assert got.id == ms.id
    finally:
        client.memory_stores.archive(ms.id)


async def test_memory_stores_list(client: OMAClient):
    page = client.memory_stores.list()
    assert isinstance(list(page), list)


async def test_memory_stores_update(client: OMAClient):
    ms = client.memory_stores.create(name="sdk-e2e-ms-before")
    try:
        updated = client.memory_stores.update(ms.id, name="sdk-e2e-ms-after")
        assert updated.name == "sdk-e2e-ms-after"
    finally:
        client.memory_stores.archive(ms.id)


async def test_memory_stores_memories_create_and_list(client: OMAClient):
    ms = client.memory_stores.create(name="sdk-e2e-ms-memories")
    try:
        mem = client.memory_stores.memories.create(
            ms.id,
            content="sdk e2e test memory content",
            path="/sdk/e2e/test",
        )
        assert mem.id

        page = client.memory_stores.memories.list(ms.id)
        mems = list(page)
        assert any(m.id == mem.id for m in mems)
    finally:
        client.memory_stores.archive(ms.id)


async def test_memory_stores_memory_versions(client: OMAClient):
    ms = client.memory_stores.create(name="sdk-e2e-ms-versions")
    try:
        page = client.memory_stores.memory_versions.list(ms.id)
        assert isinstance(list(page), list)
    finally:
        client.memory_stores.archive(ms.id)


async def test_memory_stores_archive(client: OMAClient):
    ms = client.memory_stores.create(name="sdk-e2e-ms-archive")
    archived = client.memory_stores.archive(ms.id)
    assert archived.id == ms.id
