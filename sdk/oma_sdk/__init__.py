"""
OMA Platform Python SDK.

Usage:
    import os
    os.environ["OMA_API_KEY"] = "your-key"

    from oma_sdk import OMAClient
    client = OMAClient()                          # defaults to http://localhost:8787
    client = OMAClient(base_url="http://...")     # custom server

Managed-agents resources (routed through anthropic SDK with custom base_url):
    client.agents, client.sessions, client.environments,
    client.memory_stores, client.vaults, client.skills

OMA-platform-only resources (raw httpx):
    client.dreams, client.evals, client.runtimes,
    client.integrations, client.model_cards, client.cost_report,
    client.me, client.api_keys, client.files, client.models
"""

from __future__ import annotations

import os

import anthropic
import httpx

from .resources.api_keys import ApiKeysResource
from .resources.cost_report import CostReportResource
from .resources.dreams import DreamsResource
from .resources.evals import EvalsResource
from .resources.files import FilesResource
from .resources.integrations import IntegrationsResource
from .resources.me import MeResource
from .resources.model_cards import ModelCardsResource
from .resources.models import ModelsResource
from .resources.runtimes import RuntimesResource

__all__ = ["OMAClient"]


class OMAClient:
    def __init__(self, base_url: str = "http://localhost:8787") -> None:
        api_key = os.environ["OMA_API_KEY"]
        self._anthropic = anthropic.Anthropic(api_key=api_key, base_url=base_url)
        self._http = httpx.AsyncClient(
            base_url=base_url,
            headers={"x-api-key": api_key},
            timeout=30.0,
        )
        self._base_url = base_url

        # OMA-platform-only resources
        self.dreams = DreamsResource(self._http)
        self.evals = EvalsResource(self._http)
        self.runtimes = RuntimesResource(self._http)
        self.integrations = IntegrationsResource(self._http)
        self.model_cards = ModelCardsResource(self._http)
        self.cost_report = CostReportResource(self._http)
        self.me = MeResource(self._http)
        self.api_keys = ApiKeysResource(self._http)
        self.files = FilesResource(self._http)
        self.models = ModelsResource(self._http)

    # Managed-agents resources — routed through the anthropic SDK
    @property
    def agents(self):
        return self._anthropic.beta.agents

    @property
    def sessions(self):
        return self._anthropic.beta.sessions

    @property
    def environments(self):
        return self._anthropic.beta.environments

    @property
    def memory_stores(self):
        return self._anthropic.beta.memory_stores

    @property
    def vaults(self):
        return self._anthropic.beta.vaults

    @property
    def skills(self):
        return self._anthropic.beta.skills

    async def aclose(self) -> None:
        await self._http.aclose()

    async def __aenter__(self) -> "OMAClient":
        return self

    async def __aexit__(self, *_) -> None:
        await self.aclose()
