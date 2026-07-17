"""
OMA Platform Python SDK.

Usage:
    import os
    os.environ["OMA_API_KEY"] = "your-key"

    from oma_sdk import OMAClient
    client = OMAClient()                          # reads OMA_PLATFORM_URL, else http://localhost:8787
    client = OMAClient(base_url="http://...")     # custom server

Managed-agents resources (routed through anthropic SDK with custom base_url):
    client.agents, client.sessions, client.environments,
    client.memory_stores, client.vaults, client.skills

OMA-platform-only resources (raw httpx):
    client.dreams, client.evals, client.runtimes,
    client.integrations, client.model_cards, client.cost_report,
    client.me, client.api_keys, client.files, client.models,
    client.events
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
from .resources.sessions import SessionEventsResource

from .subagent import (
    build_multiagent,
    count_thread_created,
    events_of_type,
)
from .cookbook import (
    StreamConfig,
    custom_tool_event_id,
    event_payload,
    event_type,
    message_text,
    print_stream_event,
    stop_reason_event_ids,
    stop_reason_type,
    stream_hitl_until_end_turn,
    stream_until_end_turn,
    wait_for_idle_status,
)

__all__ = [
    "OMAClient",
    "StreamConfig",
    "build_multiagent",
    "count_thread_created",
    "custom_tool_event_id",
    "event_payload",
    "event_type",
    "events_of_type",
    "message_text",
    "print_stream_event",
    "stop_reason_event_ids",
    "stop_reason_type",
    "stream_hitl_until_end_turn",
    "stream_until_end_turn",
    "wait_for_idle_status",
]


class OMAClient:
    def __init__(
        self,
        base_url: str | None = None,
        tenant_id: str | None = None,
    ) -> None:
        # Resolve base_url: explicit arg > OMA_PLATFORM_URL env var > localhost default.
        # Mirrors the convention used by the anthropic SDK (ANTHROPIC_BASE_URL).
        base_url = base_url or os.environ.get("OMA_PLATFORM_URL", "http://localhost:8787")
        api_key = os.environ["OMA_API_KEY"]
        # Build headers for tenant isolation
        anthropic_headers = {}
        http_headers = {"x-api-key": api_key}
        if tenant_id:
            anthropic_headers["x-active-tenant"] = tenant_id
            http_headers["x-active-tenant"] = tenant_id
        self._anthropic = anthropic.Anthropic(
            api_key=api_key,
            base_url=base_url,
            default_headers=anthropic_headers,
        )
        self._http = httpx.AsyncClient(
            base_url=base_url,
            headers=http_headers,
            timeout=30.0,
        )
        self._base_url = base_url
        self._tenant_id = tenant_id

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
        self.events = SessionEventsResource(self._http)

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
