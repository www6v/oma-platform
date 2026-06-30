"""
Environment Examples - High-level helper functions for environment operations.
"""

from __future__ import annotations

import os
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    import anthropic

_KEEP = os.getenv("OMA_KEEP_RESOURCES", "0") == "1"


class EnvironmentExamples:
    """Example operations for environments."""

    @staticmethod
    def create_and_retrieve(client: anthropic.Anthropic, name: str = "sdk-e2e-env-create") -> dict:
        """
        Create an environment and retrieve it to verify.
        
        Args:
            client: Anthropic client instance
            name: Name for the environment
            
        Returns:
            Dictionary with environment details
        """
        env = client.beta.environments.create(name=name)
        try:
            assert env.id
            got = client.beta.environments.retrieve(env.id)
            assert got.id == env.id
            assert got.name == env.name
            return {"environment": env, "retrieved": got}
        finally:
            if not _KEEP:
                client.beta.environments.archive(env.id)
            else:
                print(f"\n[KEEP] environment {env.id} ({name}) — archive manually when done")

    @staticmethod
    def list_environments(client: anthropic.Anthropic) -> list:
        """
        List all environments.
        
        Args:
            client: Anthropic client instance
            
        Returns:
            List of environments
        """
        page = client.beta.environments.list()
        envs = list(page)
        assert isinstance(envs, list)
        assert len(envs) >= 1  # at least the default env exists
        return envs

    @staticmethod
    def update_environment(
        client: anthropic.Anthropic,
        name_before: str = "sdk-e2e-env-before",
        name_after: str = "sdk-e2e-env-after"
    ) -> dict:
        """
        Create an environment, update it, and verify.
        
        Args:
            client: Anthropic client instance
            name_before: Initial name for the environment
            name_after: Updated name for the environment
            
        Returns:
            Dictionary with environment details
        """
        env = client.beta.environments.create(name=name_before)
        try:
            updated = client.beta.environments.update(env.id, name=name_after)
            assert updated.name == name_after
            return {"environment": env, "updated": updated}
        finally:
            if not _KEEP:
                client.beta.environments.archive(env.id)
            else:
                print(f"\n[KEEP] environment {env.id} ({name_after}) — archive manually when done")

    @staticmethod
    def delete_environment(client: anthropic.Anthropic, name: str = "sdk-e2e-env-delete") -> dict:
        """Create an environment and hard-delete it."""
        env = client.beta.environments.create(name=name)
        deleted = client.beta.environments.delete(env.id)
        assert deleted.type == "environment_deleted"
        return {"environment": env, "deleted": deleted}

    @staticmethod
    def archive_environment(client: anthropic.Anthropic, name: str = "sdk-e2e-env-archive") -> dict:
        """
        Create an environment and archive it.
        
        Args:
            client: Anthropic client instance
            name: Name for the environment
            
        Returns:
            Dictionary with environment details
        """
        env = client.beta.environments.create(name=name)
        archived = client.beta.environments.archive(env.id)
        assert archived.id == env.id
        return {"environment": env, "archived": archived}
