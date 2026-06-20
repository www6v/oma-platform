from __future__ import annotations
from typing import Any
import httpx

# Supported integration providers
PROVIDERS = ("github", "linear", "slack")


class IntegrationsResource:
    def __init__(self, http: httpx.AsyncClient) -> None:
        self._http = http

    async def list_installations(self, provider: str, **params: Any) -> dict:
        r = await self._http.get(f"/v1/integrations/{provider}/installations", params=params)
        r.raise_for_status()
        return r.json()

    async def list_publications(self, provider: str, **params: Any) -> dict:
        r = await self._http.get(f"/v1/integrations/{provider}/publications", params=params)
        r.raise_for_status()
        return r.json()
