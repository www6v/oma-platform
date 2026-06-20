"""
Vault Examples - High-level helper functions for vault operations.
"""

from __future__ import annotations

import os
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    import anthropic

_KEEP = os.getenv("OMA_KEEP_RESOURCES", "0") == "1"


class VaultExamples:
    """Example operations for vaults."""

    @staticmethod
    def create_and_retrieve(client: anthropic.Anthropic, name: str = "sdk-e2e-vault-create") -> dict:
        """
        Create a vault and retrieve it to verify.
        
        Args:
            client: Anthropic client instance
            name: Name for the vault
            
        Returns:
            Dictionary with vault details
        """
        vault = client.beta.vaults.create(
            display_name=name,
            extra_body={"name": name},
        )
        try:
            assert vault.id
            got = client.beta.vaults.retrieve(vault.id)
            assert got.id == vault.id
            return {"vault": vault, "retrieved": got}
        finally:
            if not _KEEP:
                client.beta.vaults.archive(vault.id)
            else:
                print(f"\n[KEEP] vault {vault.id} ({name}) — archive manually when done")

    @staticmethod
    def list_vaults(client: anthropic.Anthropic) -> list:
        """
        List all vaults.
        
        Args:
            client: Anthropic client instance
            
        Returns:
            List of vaults
        """
        page = client.beta.vaults.list()
        assert isinstance(list(page), list)
        return list(page)

    @staticmethod
    def list_credentials(client: anthropic.Anthropic, name: str = "sdk-e2e-vault-creds") -> dict:
        """
        Create a vault and list its credentials.
        
        Args:
            client: Anthropic client instance
            name: Name for the vault
            
        Returns:
            Dictionary with vault and credential details
        """
        vault = client.beta.vaults.create(
            display_name=name,
            extra_body={"name": name},
        )
        try:
            page = client.beta.vaults.credentials.list(vault.id)
            assert isinstance(list(page), list)
            return {"vault": vault, "credentials": list(page)}
        finally:
            if not _KEEP:
                client.beta.vaults.archive(vault.id)
            else:
                print(f"\n[KEEP] vault {vault.id} ({name}) — archive manually when done")

    @staticmethod
    def archive_vault(client: anthropic.Anthropic, name: str = "sdk-e2e-vault-archive") -> dict:
        """
        Create a vault and archive it.
        
        Args:
            client: Anthropic client instance
            name: Name for the vault
            
        Returns:
            Dictionary with vault details
        """
        vault = client.beta.vaults.create(
            display_name=name,
            extra_body={"name": name},
        )
        archived = client.beta.vaults.archive(vault.id)
        assert archived.id == vault.id
        return {"vault": vault, "archived": archived}
