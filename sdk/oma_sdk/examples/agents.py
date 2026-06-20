"""
Agent Examples - High-level helper functions for agent operations.
"""

from __future__ import annotations

import os
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    import anthropic

_KEEP = os.getenv("OMA_KEEP_RESOURCES", "0") == "1"
MODEL = {"id": "claude-sonnet-4-6"}


class AgentExamples:
    """Example operations for agents."""

    @staticmethod
    def create_and_retrieve(client: anthropic.Anthropic, name: str = "sdk-e2e-create") -> dict:
        """
        Create an agent and retrieve it to verify.
        
        Args:
            client: Anthropic client instance
            name: Name for the agent
            
        Returns:
            Dictionary with agent details
        """
        agent = client.beta.agents.create(name=name, model=MODEL)
        try:
            assert agent.id
            assert agent.name == name

            got = client.beta.agents.retrieve(agent.id)
            assert got.id == agent.id
            assert got.name == agent.name
            
            return {"agent": agent, "retrieved": got}
        finally:
            if not _KEEP:
                client.beta.agents.archive(agent.id)
            else:
                print(f"\n[KEEP] agent {agent.id} ({name}) — archive manually when done")

    @staticmethod
    def list_agents(client: anthropic.Anthropic) -> list:
        """
        List all agents.
        
        Args:
            client: Anthropic client instance
            
        Returns:
            List of agents
        """
        page = client.beta.agents.list()
        agents = list(page)
        assert isinstance(agents, list)
        return agents

    @staticmethod
    def update_agent(
        client: anthropic.Anthropic,
        name_before: str = "sdk-e2e-update-before",
        name_after: str = "sdk-e2e-update-after"
    ) -> dict:
        """
        Create an agent, update it, and verify.
        
        Args:
            client: Anthropic client instance
            name_before: Initial name for the agent
            name_after: Updated name for the agent
            
        Returns:
            Dictionary with agent details
        """
        agent = client.beta.agents.create(name=name_before, model=MODEL)
        try:
            updated = client.beta.agents.update(agent.id, version=1, name=name_after)
            assert updated.name == name_after
            return {"agent": agent, "updated": updated}
        finally:
            if not _KEEP:
                client.beta.agents.archive(agent.id)
            else:
                print(f"\n[KEEP] agent {agent.id} ({name_after}) — archive manually when done")

    @staticmethod
    def list_agent_versions(client: anthropic.Anthropic, name: str = "sdk-e2e-versions") -> dict:
        """
        Create an agent and list its versions.
        
        Args:
            client: Anthropic client instance
            name: Name for the agent
            
        Returns:
            Dictionary with agent details and versions
        """
        agent = client.beta.agents.create(name=name, model=MODEL)
        try:
            page = client.beta.agents.versions.list(agent.id)
            versions = list(page)
            assert isinstance(versions, list)
            return {"agent": agent, "versions": versions}
        finally:
            if not _KEEP:
                client.beta.agents.archive(agent.id)
            else:
                print(f"\n[KEEP] agent {agent.id} ({name}) — archive manually when done")

    @staticmethod
    def archive_agent(client: anthropic.Anthropic, name: str = "sdk-e2e-archive") -> dict:
        """
        Create an agent and archive it.
        
        Args:
            client: Anthropic client instance
            name: Name for the agent
            
        Returns:
            Dictionary with agent details
        """
        agent = client.beta.agents.create(name=name, model=MODEL)
        archived = client.beta.agents.archive(agent.id)
        assert archived.id == agent.id
        return {"agent": agent, "archived": archived}
