"""Fetch model credentials from platform internal API."""

from __future__ import annotations

import os
from typing import Any

import httpx

from oma_adapter.types import ModelConfig


def resolve_model_from_platform(
    *,
    platform_url: str,
    internal_secret: str,
    tenant_id: str,
    model_id: str,
) -> ModelConfig:
    """Resolve agent.model handle via GET /v1/internal/model_cards/resolve."""
    url = f"{platform_url.rstrip('/')}/v1/internal/model_cards/resolve"
    headers = {"x-internal-secret": internal_secret}
    params = {"tenant_id": tenant_id, "model_id": model_id}
    with httpx.Client(timeout=30.0) as client:
        resp = client.get(url, headers=headers, params=params)
        resp.raise_for_status()
        data: dict[str, Any] = resp.json()
    return ModelConfig(
        model=str(data.get("model") or model_id),
        provider=data.get("provider"),
        api_key=data.get("api_key"),
        base_url=data.get("base_url"),
        custom_headers=data.get("custom_headers"),
    )


def platform_credentials_env() -> tuple[str, str, str] | None:
    """Return (platform_url, internal_secret, tenant_id) when configured."""
    platform_url = os.environ.get("OMA_PLATFORM_URL", "").strip()
    secret = os.environ.get("OMA_INTERNAL_SECRET", "").strip()
    tenant_id = os.environ.get("OMA_TENANT_ID", "default").strip() or "default"
    if not platform_url or not secret:
        return None
    return platform_url, secret, tenant_id
