from __future__ import annotations
from typing import Any
import httpx


class ApiKeysResource:
    def __init__(self, http: httpx.AsyncClient) -> None:
        self._http = http

    async def create(self, name: str, **kwargs: Any) -> dict:
        r = await self._http.post("/v1/api_keys", json={"name": name, **kwargs})
        r.raise_for_status()
        return r.json()

    async def list(self, **params: Any) -> dict:
        r = await self._http.get("/v1/api_keys", params=params)
        r.raise_for_status()
        return r.json()

    async def delete(self, key_id: str) -> dict:
        r = await self._http.delete(f"/v1/api_keys/{key_id}")
        r.raise_for_status()
        return r.json()
