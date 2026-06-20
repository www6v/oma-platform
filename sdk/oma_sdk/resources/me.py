from __future__ import annotations
import httpx


class MeResource:
    def __init__(self, http: httpx.AsyncClient) -> None:
        self._http = http

    async def get(self) -> dict:
        r = await self._http.get("/v1/me")
        r.raise_for_status()
        return r.json()
