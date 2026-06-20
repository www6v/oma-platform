"""
Memory Store Examples - High-level helper functions for memory store operations.
"""

from __future__ import annotations

import os
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    import anthropic

_KEEP = os.getenv("OMA_KEEP_RESOURCES", "0") == "1"


class MemoryStoreExamples:
    """Example operations for memory stores."""

    @staticmethod
    def create_and_retrieve(client: anthropic.Anthropic, name: str = "sdk-e2e-ms-create") -> dict:
        """
        Create a memory store and retrieve it to verify.
        
        Args:
            client: Anthropic client instance
            name: Name for the memory store
            
        Returns:
            Dictionary with memory store details
        """
        ms = client.beta.memory_stores.create(name=name)
        try:
            assert ms.id
            got = client.beta.memory_stores.retrieve(ms.id)
            assert got.id == ms.id
            return {"memory_store": ms, "retrieved": got}
        finally:
            if not _KEEP:
                client.beta.memory_stores.archive(ms.id)
            else:
                print(f"\n[KEEP] memory_store {ms.id} ({name}) — archive manually when done")

    @staticmethod
    def list_memory_stores(client: anthropic.Anthropic) -> list:
        """
        List all memory stores.
        
        Args:
            client: Anthropic client instance
            
        Returns:
            List of memory stores
        """
        page = client.beta.memory_stores.list()
        assert isinstance(list(page), list)
        return list(page)

    @staticmethod
    def update_memory_store(
        client: anthropic.Anthropic,
        name_before: str = "sdk-e2e-ms-before",
        name_after: str = "sdk-e2e-ms-after"
    ) -> dict:
        """
        Create a memory store, update it, and verify.
        
        Args:
            client: Anthropic client instance
            name_before: Initial name for the memory store
            name_after: Updated name for the memory store
            
        Returns:
            Dictionary with memory store details
        """
        ms = client.beta.memory_stores.create(name=name_before)
        try:
            updated = client.beta.memory_stores.update(ms.id, name=name_after)
            assert updated.name == name_after
            return {"memory_store": ms, "updated": updated}
        finally:
            if not _KEEP:
                client.beta.memory_stores.archive(ms.id)
            else:
                print(f"\n[KEEP] memory_store {ms.id} ({name_after}) — archive manually when done")

    @staticmethod
    def create_and_list_memories(
        client: anthropic.Anthropic,
        name: str = "sdk-e2e-ms-memories",
        content: str = "sdk e2e test memory content",
        path: str = "/sdk/e2e/test"
    ) -> dict:
        """
        Create a memory store, add memories, and list them.
        
        Args:
            client: Anthropic client instance
            name: Name for the memory store
            content: Content for the memory
            path: Path for the memory
            
        Returns:
            Dictionary with memory store and memory details
        """
        ms = client.beta.memory_stores.create(name=name)
        try:
            mem = client.beta.memory_stores.memories.create(
                ms.id,
                content=content,
                path=path,
            )
            assert mem.id

            page = client.beta.memory_stores.memories.list(ms.id)
            mems = list(page)
            assert any(m.id == mem.id for m in mems)
            return {"memory_store": ms, "memory": mem, "memories": mems}
        finally:
            if not _KEEP:
                client.beta.memory_stores.archive(ms.id)
            else:
                print(f"\n[KEEP] memory_store {ms.id} ({name}) — archive manually when done")

    @staticmethod
    def list_memory_versions(
        client: anthropic.Anthropic,
        name: str = "sdk-e2e-ms-versions"
    ) -> dict:
        """
        Create a memory store and list its memory versions.
        
        Args:
            client: Anthropic client instance
            name: Name for the memory store
            
        Returns:
            Dictionary with memory store and version details
        """
        ms = client.beta.memory_stores.create(name=name)
        try:
            page = client.beta.memory_stores.memory_versions.list(ms.id)
            assert isinstance(list(page), list)
            return {"memory_store": ms, "versions": list(page)}
        finally:
            if not _KEEP:
                client.beta.memory_stores.archive(ms.id)
            else:
                print(f"\n[KEEP] memory_store {ms.id} ({name}) — archive manually when done")

    @staticmethod
    def archive_memory_store(client: anthropic.Anthropic, name: str = "sdk-e2e-ms-archive") -> dict:
        """
        Create a memory store and archive it.
        
        Args:
            client: Anthropic client instance
            name: Name for the memory store
            
        Returns:
            Dictionary with memory store details
        """
        ms = client.beta.memory_stores.create(name=name)
        archived = client.beta.memory_stores.archive(ms.id)
        assert archived.id == ms.id
        return {"memory_store": ms, "archived": archived}
