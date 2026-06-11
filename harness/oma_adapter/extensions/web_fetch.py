"""OMA web_fetch tool registered via piPy extension (harness/tools.ts parity)."""

from __future__ import annotations

import os
import re
from html.parser import HTMLParser
from typing import Any
from urllib.parse import urlparse

import httpx

from pi_agent.types import AgentToolResult
from pi_ai.types import TextContent

MAX_TOOL_RESULT_CHARS = 50_000
DEFAULT_MAX_LENGTH = 50_000
FETCH_TIMEOUT_SEC = 20.0
USER_AGENT = "OMA-Agent/1.0 (+web_fetch)"


class _HtmlTextExtractor(HTMLParser):
    """Strip chrome tags and flatten HTML to readable text."""

    _BLOCK_TAGS = frozenset(
        {"p", "br", "div", "li", "h1", "h2", "h3", "h4", "h5", "h6", "tr"}
    )
    _SKIP_TAGS = frozenset({"script", "style", "nav", "footer", "header"})

    def __init__(self) -> None:
        super().__init__()
        self._parts: list[str] = []
        self._skip_depth = 0

    def handle_starttag(self, tag: str, attrs: list[tuple[str, str | None]]) -> None:
        del attrs
        lowered = tag.lower()
        if lowered in self._SKIP_TAGS:
            self._skip_depth += 1
            return
        if lowered in self._BLOCK_TAGS:
            self._parts.append("\n")

    def handle_endtag(self, tag: str) -> None:
        lowered = tag.lower()
        if lowered in self._SKIP_TAGS and self._skip_depth > 0:
            self._skip_depth -= 1
            return
        if lowered in self._BLOCK_TAGS:
            self._parts.append("\n")

    def handle_data(self, data: str) -> None:
        if self._skip_depth == 0:
            self._parts.append(data)

    def text(self) -> str:
        raw = "".join(self._parts)
        return re.sub(r"\n{3,}", "\n\n", raw).strip()


def _html_to_text(html: str) -> str:
    parser = _HtmlTextExtractor()
    parser.feed(html)
    return parser.text()


def _truncate(text: str, cap: int) -> str:
    if len(text) <= cap:
        return text
    return text[:cap] + f"\n\n…[truncated to {cap} chars]"


def _host_allowed(hostname: str) -> bool:
    if os.environ.get("OMA_NETWORKING_TYPE") != "limited":
        return True
    allowed_raw = os.environ.get("OMA_NETWORKING_ALLOWED_HOSTS", "")
    allowed_hosts = [
        item.strip() for item in allowed_raw.split(",") if item.strip()
    ]
    if not allowed_hosts:
        return False
    for host in allowed_hosts:
        if hostname == host or hostname.endswith(f".{host}"):
            return True
    return False


def _validate_url(url: str) -> tuple[str | None, str | None]:
    try:
        parsed = urlparse(url)
    except ValueError:
        return None, "Error: Invalid URL"
    if parsed.scheme not in {"http", "https"} or not parsed.netloc:
        return None, "Error: Invalid URL"
    if not _host_allowed(parsed.hostname or ""):
        allowed_raw = os.environ.get("OMA_NETWORKING_ALLOWED_HOSTS", "")
        allowed_hosts = [
            item.strip() for item in allowed_raw.split(",") if item.strip()
        ]
        host = parsed.hostname or ""
        return None, (
            f'Error: Host "{host}" is not allowed. '
            f"Allowed hosts: {', '.join(allowed_hosts)}"
        )
    return url, None


async def _fetch_url(url: str, cap: int) -> str:
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
        content_type = (response.headers.get("content-type") or "").lower()
        body = response.text
        if "html" in content_type or body.lstrip().startswith("<"):
            markdown = _html_to_text(body)
        else:
            markdown = body
        if not markdown.strip():
            return (
                "[NOTE: empty response body for this URL]\n\n"
                f"HTTP {response.status_code}"
            )
        return _truncate(markdown, cap)


class WebFetchTool:
    """Fetch a URL and return page content as markdown-ish text."""

    name = "web_fetch"
    description = (
        "Fetch a URL and return clean markdown — strips boilerplate "
        "(nav/ads/scripts), preserves headings and links. Use this as the "
        "default way to read web pages; fall back to browser_* tools only if "
        "you need to interact (click, fill forms) or if the page is "
        "JS-rendered (SPA) and returns empty markdown."
    )
    parameters: dict[str, Any] = {
        "type": "object",
        "properties": {
            "url": {"type": "string", "description": "URL to fetch"},
            "max_length": {
                "type": "integer",
                "description": (
                    "Truncate returned markdown to this many chars "
                    f"(default {DEFAULT_MAX_LENGTH})"
                ),
            },
        },
        "required": ["url"],
    }
    execution_mode = "parallel"

    async def execute(
        self,
        tool_call_id: str,
        args: dict[str, Any],
        signal: Any = None,
        on_update: Any = None,
    ) -> AgentToolResult:
        del tool_call_id, signal, on_update
        url_raw = args.get("url")
        if not isinstance(url_raw, str) or not url_raw.strip():
            return AgentToolResult(
                content=[TextContent(text="Error: url is required")],
                is_error=True,
            )
        url, err = _validate_url(url_raw.strip())
        if err is not None:
            return AgentToolResult(
                content=[TextContent(text=err)],
                is_error=True,
            )
        assert url is not None

        max_length_raw = args.get("max_length", DEFAULT_MAX_LENGTH)
        try:
            cap = int(max_length_raw)
        except (TypeError, ValueError):
            cap = DEFAULT_MAX_LENGTH
        cap = min(max(cap, 1), MAX_TOOL_RESULT_CHARS)

        try:
            text = await _fetch_url(url, cap)
        except httpx.HTTPStatusError as exc:
            status = exc.response.status_code
            msg = f"Error: HTTP {status} fetching {url}"
            return AgentToolResult(
                content=[TextContent(text=msg)],
                is_error=True,
            )
        except httpx.HTTPError as exc:
            msg = f"Error: {exc}"
            return AgentToolResult(
                content=[TextContent(text=msg)],
                is_error=True,
            )
        except Exception as exc:  # noqa: BLE001 — tool errors return to model
            msg = f"Error: {exc}"
            return AgentToolResult(
                content=[TextContent(text=msg)],
                is_error=True,
            )

        return AgentToolResult(content=[TextContent(text=text)])


def register(pi: Any) -> None:
    """piPy extension entrypoint — mirrors pi ``registerTool`` pattern."""
    pi.register_tool(WebFetchTool())
