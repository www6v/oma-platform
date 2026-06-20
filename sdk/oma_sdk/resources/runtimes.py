from __future__ import annotations
from typing import Any
import httpx


class RuntimesResource:
    def __init__(self, http: httpx.AsyncClient) -> None:
        self._http = http

    async def list(self, **params: Any) -> dict:
        r = await self._http.get("/v1/runtimes", params=params)
        r.raise_for_status()
        return r.json()

    async def retrieve(self, runtime_id: str) -> dict:
        r = await self._http.get(f"/v1/runtimes/{runtime_id}")
        r.raise_for_status()
        return r.json()
