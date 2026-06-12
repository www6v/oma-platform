"""web_fetch orchestration: fetch → toMarkdown → aux summarize → .web offload."""

from __future__ import annotations

import asyncio
import hashlib
import json
import os
import shlex
from pathlib import Path
from typing import Any
from urllib.parse import urlparse

import httpx

from oma_adapter.web_fetch.runtime import WebFetchRuntime, get_web_fetch_runtime
from oma_adapter.web_fetch.summarize import summarize_page_markdown
from oma_adapter.web_fetch.to_markdown import html_to_markdown

FETCH_TIMEOUT_SEC = 20.0
USER_AGENT = "OMA-Agent/1.0 (+web_fetch)"
SUMMARY_THRESHOLD = 5000
DEFAULT_MAX_LENGTH = 50_000
MAX_TOOL_RESULT_CHARS = 50_000


def _truncate(text: str, cap: int) -> str:
    if len(text) <= cap:
        return text
    return text[:cap] + f"\n\n…[truncated to {cap} chars]"


def _networking_from_environment(
    environment: dict[str, Any] | None,
) -> tuple[str, list[str]]:
    net_type = os.environ.get("OMA_NETWORKING_TYPE", "open")
    allowed_raw = os.environ.get("OMA_NETWORKING_ALLOWED_HOSTS", "")
    allowed = [item.strip() for item in allowed_raw.split(",") if item.strip()]
    if not environment:
        return net_type, allowed

    config = environment.get("config", {})
    if isinstance(config, str):
        try:
            config = json.loads(config)
        except json.JSONDecodeError:
            config = {}
    if not isinstance(config, dict):
        return net_type, allowed

    networking = config.get("networking") or {}
    if not isinstance(networking, dict):
        return net_type, allowed
    net_type = str(networking.get("type") or net_type)
    hosts = networking.get("allowed_hosts") or allowed
    if isinstance(hosts, list):
        allowed = [str(h).strip() for h in hosts if str(h).strip()]
    return net_type, allowed


def _host_allowed(hostname: str, environment: dict[str, Any] | None) -> bool:
    net_type, allowed_hosts = _networking_from_environment(environment)
    if net_type != "limited":
        return True
    if not allowed_hosts:
        return False
    for host in allowed_hosts:
        if hostname == host or hostname.endswith(f".{host}"):
            return True
    return False


def validate_url(url: str, environment: dict[str, Any] | None) -> tuple[str | None, str | None]:
    try:
        parsed = urlparse(url)
    except ValueError:
        return None, "Error: Invalid URL"
    if parsed.scheme not in {"http", "https"} or not parsed.netloc:
        return None, "Error: Invalid URL"
    host = parsed.hostname or ""
    if not _host_allowed(host, environment):
        _, allowed_hosts = _networking_from_environment(environment)
        return None, (
            f'Error: Host "{host}" is not allowed. '
            f"Allowed hosts: {', '.join(allowed_hosts)}"
        )
    return url, None


def web_cache_path(workdir: str, url: str) -> Path:
    digest = hashlib.sha256(url.encode()).hexdigest()[:16]
    return Path(workdir) / ".web" / f"{digest}.md"


def web_cache_display_path(workdir: str, url: str) -> str:
    """Sandbox-relative path shown to the model (OMA: /workspace/.web/…)."""
    rel = web_cache_path(workdir, url).name
    return f"/workspace/.web/{rel}"


async def _curl_fallback(url: str, cap: int) -> str:
    cmd = (
        f"curl -sL -m 30 {shlex.quote(url)} | head -c {cap}"
    )
    proc = await asyncio.create_subprocess_shell(
        cmd,
        stdout=asyncio.subprocess.PIPE,
        stderr=asyncio.subprocess.PIPE,
    )
    stdout, _stderr = await proc.communicate()
    raw = stdout.decode("utf-8", errors="replace")
    return (
        "[NOTE: markdown extraction unavailable for this URL — "
        "returning raw response. Look for the actual content "
        "between HTML tags.]\n\n"
        f"{_truncate(raw, cap)}"
    )


async def _fetch_bytes(url: str) -> tuple[bytes, str, str]:
    headers = {
        "User-Agent": USER_AGENT,
        "Accept": (
            "text/html, application/xhtml+xml, application/xml;q=0.9, "
            "*/*;q=0.8"
        ),
    }
    async with httpx.AsyncClient(
        follow_redirects=True,
        timeout=FETCH_TIMEOUT_SEC,
    ) as client:
        response = await client.get(url, headers=headers)
        response.raise_for_status()
        content_type = response.headers.get("content-type") or "text/html"
        return response.content, content_type, str(response.url)


async def _fetch_to_markdown(url: str, cap: int) -> tuple[str, bool]:
    """Return (markdown, is_raw_fallback)."""
    try:
        body, content_type, _final_url = await _fetch_bytes(url)
        text = body.decode("utf-8", errors="replace")
        markdown = html_to_markdown(text, content_type)
        if markdown is not None and markdown.strip():
            return markdown, False
    except Exception:
        pass
    raw = await _curl_fallback(url, cap)
    return raw, True


def _write_offload(workdir: str, url: str, markdown: str) -> Path | None:
    path = web_cache_path(workdir, url)
    try:
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(markdown, encoding="utf-8")
        return path
    except OSError:
        return None


async def fetch_web_content(url: str, cap: int) -> str:
    runtime = get_web_fetch_runtime()
    url, err = validate_url(url, runtime.environment)
    if err is not None:
        return err
    assert url is not None

    cap = min(max(cap, 1), MAX_TOOL_RESULT_CHARS)
    markdown, is_raw = await _fetch_to_markdown(url, cap)

    if (
        not is_raw
        and runtime.aux_model is not None
        and len(markdown) > SUMMARY_THRESHOLD
    ):
        raw_path = web_cache_display_path(runtime.workdir, url)
        _write_offload(runtime.workdir, url, markdown)
        try:
            summary, _usage = await summarize_page_markdown(
                url=url,
                markdown=markdown,
                aux_cfg=runtime.aux_model,
                runtime=runtime,
            )
            compression_pct = round((len(summary) / len(markdown)) * 100)
            return json.dumps(
                {
                    "url": url,
                    "content": summary,
                    "_meta": {
                        "extractor": runtime.aux_model.model,
                        "compression": (
                            f"{len(markdown)} → {len(summary)} chars "
                            f"({compression_pct}%)"
                        ),
                        "raw_at": raw_path,
                        "hint": (
                            f"Use `read {raw_path}` (with offset/limit) to see "
                            "full original markdown if the summary is missing "
                            "detail."
                        ),
                    },
                },
                indent=2,
            )
        except Exception as exc:
            return json.dumps(
                {
                    "url": url,
                    "content": _truncate(markdown, cap),
                    "_meta": {
                        "summary_failed": True,
                        "summary_error": str(exc),
                    },
                },
                indent=2,
            )

    if not markdown.strip():
        return "[NOTE: empty response body for this URL]"
    return _truncate(markdown, cap)
