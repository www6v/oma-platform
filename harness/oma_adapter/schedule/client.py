"""HTTP client for oma-server session wakeup internal API."""

from __future__ import annotations

import json
from typing import Any
from urllib.parse import urljoin

import httpx

from oma_adapter.schedule.runtime import get_schedule_runtime


def _base_url() -> str:
    runtime = get_schedule_runtime()
    base = (runtime.platform_base or "").rstrip("/")
    if not base:
        raise RuntimeError(
            "schedule tools unavailable: platform_base not configured"
        )
    return base


def _headers() -> dict[str, str]:
    runtime = get_schedule_runtime()
    secret = runtime.internal_secret or ""
    if not secret:
        raise RuntimeError(
            "schedule tools unavailable: OMA_INTERNAL_SECRET not configured"
        )
    return {
        "Content-Type": "application/json",
        "x-internal-secret": secret,
    }


def _session_id() -> str:
    runtime = get_schedule_runtime()
    if not runtime.session_id:
        raise RuntimeError("schedule tools unavailable: session_id missing")
    return runtime.session_id


async def schedule_wakeup(args: dict[str, Any]) -> dict[str, Any]:
    session_id = _session_id()
    url = urljoin(
        _base_url() + "/",
        f"v1/internal/sessions/{session_id}/wakeups",
    )
    body = {k: v for k, v in args.items() if v is not None}
    async with httpx.AsyncClient(timeout=30.0) as client:
        resp = await client.post(url, headers=_headers(), json=body)
    if resp.status_code >= 400:
        detail = resp.text.strip() or resp.reason_phrase
        try:
            payload = resp.json()
            if isinstance(payload, dict) and payload.get("error"):
                detail = str(payload["error"])
        except json.JSONDecodeError:
            pass
        raise RuntimeError(detail)
    return resp.json()


async def cancel_wakeup(schedule_id: str) -> dict[str, bool]:
    session_id = _session_id()
    url = urljoin(
        _base_url() + "/",
        f"v1/internal/sessions/{session_id}/wakeups/{schedule_id}",
    )
    async with httpx.AsyncClient(timeout=30.0) as client:
        resp = await client.delete(url, headers=_headers())
    if resp.status_code >= 400:
        raise RuntimeError(resp.text.strip() or resp.reason_phrase)
    data = resp.json()
    if isinstance(data, dict) and "cancelled" in data:
        return {"cancelled": bool(data["cancelled"])}
    return {"cancelled": False}


async def list_wakeups() -> dict[str, Any]:
    session_id = _session_id()
    url = urljoin(
        _base_url() + "/",
        f"v1/internal/sessions/{session_id}/wakeups",
    )
    async with httpx.AsyncClient(timeout=30.0) as client:
        resp = await client.get(url, headers=_headers())
    if resp.status_code >= 400:
        raise RuntimeError(resp.text.strip() or resp.reason_phrase)
    data = resp.json()
    if isinstance(data, dict) and "schedules" in data:
        return data
    return {"schedules": []}
