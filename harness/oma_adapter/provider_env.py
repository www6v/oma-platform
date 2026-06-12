"""Provider API key / base URL env overrides for a turn."""

from __future__ import annotations

import os
from contextlib import contextmanager
from typing import Iterator

from oma_adapter.types import ModelConfig


@contextmanager
def provider_env(model: ModelConfig | None) -> Iterator[None]:
    if model is None or not model.api_key:
        yield
        return
    provider = (model.provider or "").lower()
    env_keys = ["ANTHROPIC_API_KEY"]
    base_url_key = "ANTHROPIC_BASE_URL"
    if provider.startswith("oai") or provider == "openai":
        env_keys = ["OPENAI_API_KEY", "ANTHROPIC_API_KEY"]
        base_url_key = "OPENAI_BASE_URL"
    saved: dict[str, str | None] = {
        key: os.environ.get(key) for key in env_keys
    }
    if model.base_url:
        saved[base_url_key] = os.environ.get(base_url_key)
    try:
        os.environ[env_keys[0]] = model.api_key
        if model.base_url:
            os.environ[base_url_key] = model.base_url
        yield
    finally:
        for key, value in saved.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value
