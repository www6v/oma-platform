"""
Misc Examples - High-level helper functions for miscellaneous operations.
"""

from __future__ import annotations

from typing import TYPE_CHECKING

import httpx

if TYPE_CHECKING:
    import anthropic


class MiscExamples:
    """Example operations for miscellaneous endpoints."""

    @staticmethod
    def get_me(client: anthropic.Anthropic) -> dict:
        """
        Get current user information.
        
        Args:
            client: Anthropic client instance
            
        Returns:
            Dictionary with user information
        """
        resp = client.get("/v1/me", cast_to=httpx.Response)
        data = resp.json()
        assert "user" in data
        assert "id" in data["user"]
        assert "email" in data["user"]
        return data

    @staticmethod
    def list_models(client: anthropic.Anthropic) -> dict:
        """
        List available models.
        
        Args:
            client: Anthropic client instance
            
        Returns:
            Dictionary with model list
        """
        resp = client.get("/v1/models/list", cast_to=httpx.Response)
        data = resp.json()
        assert "data" in data
        ids = [m["id"] for m in data["data"]]
        assert any("claude" in i for i in ids)
        return data

    @staticmethod
    def list_api_keys(client: anthropic.Anthropic) -> dict:
        """
        List API keys.
        
        Args:
            client: Anthropic client instance
            
        Returns:
            Dictionary with API key list
        """
        resp = client.get("/v1/api_keys", cast_to=httpx.Response)
        data = resp.json()
        assert "data" in data
        return data

    @staticmethod
    def create_and_delete_api_key(client: anthropic.Anthropic, name: str = "sdk-e2e-key") -> dict:
        """
        Create an API key and then delete it.
        
        Args:
            client: Anthropic client instance
            name: Name for the API key
            
        Returns:
            Dictionary with API key details
        """
        resp = client.post("/v1/api_keys", cast_to=httpx.Response, body={"name": name})
        data = resp.json()
        assert "id" in data
        key_id = data["id"]
        client.delete(f"/v1/api_keys/{key_id}", cast_to=httpx.Response)
        return data

    @staticmethod
    def list_github_installations(client: anthropic.Anthropic) -> dict:
        """
        List GitHub integration installations.
        
        Args:
            client: Anthropic client instance
            
        Returns:
            Dictionary with GitHub installations
        """
        resp = client.get("/v1/integrations/github/installations", cast_to=httpx.Response)
        data = resp.json()
        assert "data" in data
        return data

    @staticmethod
    def list_linear_installations(client: anthropic.Anthropic) -> dict:
        """
        List Linear integration installations.
        
        Args:
            client: Anthropic client instance
            
        Returns:
            Dictionary with Linear installations
        """
        resp = client.get("/v1/integrations/linear/installations", cast_to=httpx.Response)
        data = resp.json()
        assert "data" in data
        return data

    @staticmethod
    def list_dreams(client: anthropic.Anthropic) -> dict:
        """
        List dreams.
        
        Args:
            client: Anthropic client instance
            
        Returns:
            Dictionary with dreams list
        """
        resp = client.get("/v1/dreams", cast_to=httpx.Response)
        data = resp.json()
        assert "data" in data
        assert isinstance(data["data"], list)
        return data

    @staticmethod
    def get_cost_report(client: anthropic.Anthropic) -> dict:
        """
        Get cost report.
        
        Args:
            client: Anthropic client instance
            
        Returns:
            Dictionary with cost report
        """
        resp = client.get("/v1/cost_report", cast_to=httpx.Response)
        data = resp.json()
        assert "type" in data
        assert data["type"] == "cost_report"
        assert "usage" in data
        return data

    @staticmethod
    def list_files(client: anthropic.Anthropic) -> list:
        """
        List files.
        
        Args:
            client: Anthropic client instance
            
        Returns:
            List of files
        """
        page = client.beta.files.list()
        assert isinstance(list(page), list)
        return list(page)
