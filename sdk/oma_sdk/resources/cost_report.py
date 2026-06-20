from __future__ import annotations
from typing import Any
import httpx


class CostReportResource:
    def __init__(self, http: httpx.AsyncClient) -> None:
        self._http = http

    async def get(self, **params: Any) -> dict:
        r = await self._http.get("/v1/cost_report", params=params)
        r.raise_for_status()
        return r.json()
