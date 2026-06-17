"""HTTP bridge to oma-server for session events (append / enqueue)."""

from __future__ import annotations

import json
from typing import Any
from urllib.parse import urljoin

import httpx

from pi_team.runtime import get_team_runtime


class TeamPlatformError(RuntimeError):
    pass


def _base_url() -> str:
    runtime = get_team_runtime()
    base = (runtime.platform_base or "").rstrip("/")
    if not base:
        raise TeamPlatformError(
            "team tools unavailable: platform_base not configured"
        )
    return base


def _headers() -> dict[str, str]:
    runtime = get_team_runtime()
    secret = runtime.internal_secret or ""
    if not secret:
        raise TeamPlatformError(
            "team tools unavailable: OMA_INTERNAL_SECRET not configured"
        )
    return {
        "Content-Type": "application/json",
        "x-internal-secret": secret,
    }


def _session_id() -> str:
    runtime = get_team_runtime()
    if not runtime.session_id:
        raise TeamPlatformError("team tools unavailable: session_id missing")
    return runtime.session_id


async def _post_batch(body: dict[str, Any]) -> None:
    session_id = _session_id()
    url = urljoin(
        _base_url() + "/",
        f"v1/internal/sessions/{session_id}/events/batch",
    )
    async with httpx.AsyncClient(timeout=60.0) as client:
        resp = await client.post(url, headers=_headers(), json=body)
    if resp.status_code >= 400:
        detail = resp.text.strip() or resp.reason_phrase
        try:
            payload = resp.json()
            if isinstance(payload, dict) and payload.get("error"):
                detail = str(payload["error"])
        except json.JSONDecodeError:
            pass
        raise TeamPlatformError(detail)


async def append_session_events(events: list[dict[str, Any]]) -> None:
    if not events:
        return
    await _post_batch({"events": events, "enqueue": False})


async def enqueue_session_events(
    events: list[dict[str, Any]],
    *,
    run_turn: bool = True,
) -> None:
    if not events:
        return
    await _post_batch(
        {"events": events, "enqueue": True, "run_turn": run_turn}
    )
