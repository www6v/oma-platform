"""Outbound vault HTTP proxy wiring for sandbox subprocesses."""

from __future__ import annotations

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
    """Write sandbox-local .curlrc for vault outbound proxy.

    Do not set HTTP_PROXY on the harness process: piPy LLM clients would
    route model API traffic through the outbound proxy and the turn would
    fail with no assistant output. Bash/curl in the sandbox reads .curlrc.
    """
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
    return {}


def clear_outbound_proxy_for_turn(saved: dict[str, str | None]) -> None:
    del saved
