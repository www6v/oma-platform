"""Outbound vault HTTP proxy wiring for sandbox subprocesses."""

from __future__ import annotations

import os
from pathlib import Path


def normalize_outbound_proxy_addr(addr: str | None) -> str | None:
    if not addr:
        return None
    trimmed = addr.strip()
    if not trimmed:
        return None
    if trimmed.startswith(":"):
        return f"127.0.0.1{trimmed}"
    return trimmed


def setup_outbound_proxy_for_turn(
    *,
    workdir: str,
    session_id: str,
    proxy_addr: str | None,
    proxy_api_key: str | None,
) -> dict[str, str | None]:
    """Configure HTTP proxy env + .curlrc; returns saved env for restore."""
    host = normalize_outbound_proxy_addr(proxy_addr)
    if not host or not proxy_api_key:
        return {}

    proxy_url = f"http://{host}"
    workdir_path = Path(workdir)
    workdir_path.mkdir(parents=True, exist_ok=True)
    curlrc = workdir_path / ".curlrc"
    curlrc.write_text(
        "\n".join(
            [
                f'proxy = "{proxy_url}"',
                f'proxy-header = "X-OMA-Session-Id: {session_id}"',
                (
                    'proxy-header = "Proxy-Authorization: Bearer '
                    f'{proxy_api_key}"'
                ),
            ],
        )
        + "\n",
        encoding="utf-8",
    )

    saved: dict[str, str | None] = {}
    for key in (
        "HTTP_PROXY",
        "http_proxy",
        "HTTPS_PROXY",
        "https_proxy",
    ):
        saved[key] = os.environ.get(key)
        os.environ[key] = proxy_url
    return saved


def clear_outbound_proxy_for_turn(saved: dict[str, str | None]) -> None:
    for key, value in saved.items():
        if value is None:
            os.environ.pop(key, None)
        else:
            os.environ[key] = value
