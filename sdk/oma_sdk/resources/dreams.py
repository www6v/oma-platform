from __future__ import annotations
from typing import Any
import httpx

_BETA = "managed-agents-2026-04-01,dreaming-2026-04-21"


class DreamsResource:
    def __init__(self, http: httpx.AsyncClient) -> None:
        self._http = http

    async def create(self, agent_id: str, **kwargs: Any) -> dict:
        r = await self._http.post(
            "/v1/dreams",
            json={"agent_id": agent_id, **kwargs},
            headers={"anthropic-beta": _BETA},
        )
        r.raise_for_status()
        return r.json()

    async def list(self, agent_id: str | None = None, **params: Any) -> dict:
        query: dict = {**params}
        if agent_id:
            query["agent_id"] = agent_id
        r = await self._http.get("/v1/dreams", params=query, headers={"anthropic-beta": _BETA})
        r.raise_for_status()
        return r.json()

    async def retrieve(self, dream_id: str) -> dict:
        r = await self._http.get(f"/v1/dreams/{dream_id}", headers={"anthropic-beta": _BETA})
        r.raise_for_status()
        return r.json()

    async def cancel(self, dream_id: str) -> dict:
        r = await self._http.post(f"/v1/dreams/{dream_id}/cancel", headers={"anthropic-beta": _BETA})
        r.raise_for_status()
        return r.json()

    async def archive(self, dream_id: str) -> dict:
        r = await self._http.post(f"/v1/dreams/{dream_id}/archive", headers={"anthropic-beta": _BETA})
        r.raise_for_status()
        return r.json()
