from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import httpx
import pytest

from oma_adapter.extensions.web_fetch import WebFetchTool
from oma_adapter.types import ModelConfig
from oma_adapter.web_fetch.core import (
    SUMMARY_THRESHOLD,
    fetch_web_content,
    validate_url,
    web_cache_path,
)
from oma_adapter.web_fetch.runtime import WebFetchRuntime, configure_web_fetch, clear_web_fetch_runtime
from oma_adapter.web_fetch.to_markdown import html_to_markdown


def test_html_to_markdown_strips_scripts() -> None:
    html = (
        "<html><head><script>bad()</script></head>"
        "<body><nav>Menu</nav><h1>Title</h1><p>Hello <b>world</b></p></body></html>"
    )
    md = html_to_markdown(html)
    assert md is not None
    assert "bad()" not in md
    assert "Menu" not in md
    assert "Hello" in md
    assert "world" in md


def test_validate_url_rejects_invalid() -> None:
    _, err = validate_url("not-a-url", None)
    assert err == "Error: Invalid URL"


def test_validate_url_limited_networking_from_environment() -> None:
    env = {
        "config": {
            "networking": {
                "type": "limited",
                "allowed_hosts": ["example.com"],
            },
        },
    }
    _, err = validate_url("https://blocked.test/page", env)
    assert err is not None
    assert "blocked.test" in err
    url, err_ok = validate_url("https://api.example.com/page", env)
    assert err_ok is None
    assert url == "https://api.example.com/page"


@pytest.mark.asyncio
async def test_web_fetch_tool_fetches_html(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    class FakeResponse:
        status_code = 200
        headers = {"content-type": "text/html; charset=utf-8"}

        @property
        def content(self) -> bytes:
            return b"<html><body><p>Page body</p></body></html>"

        @property
        def url(self) -> str:
            return "https://example.com"

        def raise_for_status(self) -> None:
            return None

    class FakeClient:
        def __init__(self, *args: Any, **kwargs: Any) -> None:
            del args, kwargs

        async def __aenter__(self) -> FakeClient:
            return self

        async def __aexit__(self, *args: Any) -> None:
            del args

        async def get(self, url: str, headers: dict[str, str]) -> FakeResponse:
            del headers
            assert url == "https://example.com"
            return FakeResponse()

    monkeypatch.setattr(httpx, "AsyncClient", FakeClient)
    configure_web_fetch(WebFetchRuntime(workdir="/tmp"))
    try:
        tool = WebFetchTool()
        result = await tool.execute("tc1", {"url": "https://example.com"})
        assert result.is_error is False
        assert "Page body" in result.content[0].text
    finally:
        clear_web_fetch_runtime()


@pytest.mark.asyncio
async def test_web_fetch_respects_max_length(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    long_body = "x" * 100

    class FakeResponse:
        status_code = 200
        headers = {"content-type": "text/plain"}

        @property
        def content(self) -> bytes:
            return long_body.encode()

        @property
        def url(self) -> str:
            return "https://example.com"

        def raise_for_status(self) -> None:
            return None

    class FakeClient:
        def __init__(self, *args: Any, **kwargs: Any) -> None:
            del args, kwargs

        async def __aenter__(self) -> FakeClient:
            return self

        async def __aexit__(self, *args: Any) -> None:
            del args

        async def get(self, url: str, headers: dict[str, str]) -> FakeResponse:
            del url, headers
            return FakeResponse()

    monkeypatch.setattr(httpx, "AsyncClient", FakeClient)
    configure_web_fetch(WebFetchRuntime(workdir="/tmp"))
    try:
        tool = WebFetchTool()
        result = await tool.execute(
            "tc2",
            {"url": "https://example.com", "max_length": 20},
        )
        assert len(result.content[0].text) > 20
        assert "truncated to 20 chars" in result.content[0].text
    finally:
        clear_web_fetch_runtime()


@pytest.mark.asyncio
async def test_web_fetch_offloads_and_summarizes(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    long_md = "# Title\n\n" + ("fact line\n" * (SUMMARY_THRESHOLD // 10))

    class FakeResponse:
        status_code = 200
        headers = {"content-type": "text/html; charset=utf-8"}

        @property
        def content(self) -> bytes:
            return b"<html><body><h1>Title</h1></body></html>"

        @property
        def url(self) -> str:
            return "https://example.com/big"

        def raise_for_status(self) -> None:
            return None

    class FakeClient:
        def __init__(self, *args: Any, **kwargs: Any) -> None:
            del args, kwargs

        async def __aenter__(self) -> FakeClient:
            return self

        async def __aexit__(self, *args: Any) -> None:
            del args

        async def get(self, url: str, headers: dict[str, str]) -> FakeResponse:
            del url, headers
            return FakeResponse()

    async def fake_summarize(**kwargs: Any) -> tuple[str, dict[str, int]]:
        del kwargs
        return "short summary", {"input": 10, "output": 5}

    monkeypatch.setattr(httpx, "AsyncClient", FakeClient)
    monkeypatch.setattr(
        "oma_adapter.web_fetch.core.html_to_markdown",
        lambda _html, _ct: long_md,
    )
    monkeypatch.setattr(
        "oma_adapter.web_fetch.core.summarize_page_markdown",
        fake_summarize,
    )

    url = "https://example.com/big"
    configure_web_fetch(
        WebFetchRuntime(
            workdir=str(tmp_path),
            aux_model=ModelConfig(model="faux/test", provider="faux"),
        ),
    )
    try:
        out = await fetch_web_content(url, 50_000)
        payload = json.loads(out)
        assert payload["content"] == "short summary"
        assert "/workspace/.web/" in payload["_meta"]["raw_at"]
        cache = web_cache_path(str(tmp_path), url)
        assert cache.is_file()
        assert cache.read_text(encoding="utf-8") == long_md
    finally:
        clear_web_fetch_runtime()
