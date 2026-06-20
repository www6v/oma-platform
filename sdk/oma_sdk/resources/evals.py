from __future__ import annotations
from typing import Any
import httpx


class EvalsResource:
    def __init__(self, http: httpx.AsyncClient) -> None:
        self._http = http

    async def create(self, **kwargs: Any) -> dict:
        r = await self._http.post("/v1/evals", json=kwargs)
        r.raise_for_status()
        return r.json()

    async def list(self, **params: Any) -> dict:
        r = await self._http.get("/v1/evals", params=params)
        r.raise_for_status()
        return r.json()

    async def retrieve(self, eval_id: str) -> dict:
        r = await self._http.get(f"/v1/evals/{eval_id}")
        r.raise_for_status()
        return r.json()
