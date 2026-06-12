"""Per-turn runtime for web_fetch (workdir, aux model, environment, events)."""

from __future__ import annotations

import os
from dataclasses import dataclass
from typing import Any, Awaitable, Callable

from oma_adapter.types import ModelConfig

EmitEventFn = Callable[[dict[str, Any]], Awaitable[None]]


@dataclass
class WebFetchRuntime:
    workdir: str
    aux_model: ModelConfig | None = None
    environment: dict[str, Any] | None = None
    emit_event: EmitEventFn | None = None
    outbound_proxy_url: str | None = None
    outbound_proxy_api_key: str | None = None
    session_id: str | None = None


_runtime: WebFetchRuntime | None = None


def configure_web_fetch(runtime: WebFetchRuntime) -> None:
    global _runtime
    _runtime = runtime


def get_web_fetch_runtime() -> WebFetchRuntime:
    if _runtime is not None:
        return _runtime
    return WebFetchRuntime(workdir=os.getcwd())


def clear_web_fetch_runtime() -> None:
    global _runtime
    _runtime = None
