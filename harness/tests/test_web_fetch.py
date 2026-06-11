from __future__ import annotations

from typing import Any

import httpx
import pytest

from oma_adapter.extensions.web_fetch import (
    WebFetchTool,
    _html_to_text,
    _validate_url,
)


def test_html_to_text_strips_scripts() -> None:
    html = (
        "<html><head><script>bad()</script></head>"
        "<body><nav>Menu</nav><p>Hello <b>world</b></p></body></html>"
    )
    text = _html_to_text(html)
    assert "bad()" not in text
    assert "Menu" not in text
    assert "Hello" in text
    assert "world" in text


def test_validate_url_rejects_invalid() -> None:
    _, err = _validate_url("not-a-url")
    assert err == "Error: Invalid URL"


def test_validate_url_limited_networking(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("OMA_NETWORKING_TYPE", "limited")
    monkeypatch.setenv("OMA_NETWORKING_ALLOWED_HOSTS", "example.com")
    _, err = _validate_url("https://blocked.test/page")
    assert err is not None
    assert "blocked.test" in err
    url, err_ok = _validate_url("https://api.example.com/page")
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
        def text(self) -> str:
            return "<html><body><p>Page body</p></body></html>"

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

    tool = WebFetchTool()
    result = await tool.execute("tc1", {"url": "https://example.com"})
    assert result.is_error is False
    assert result.content[0].text == "Page body"


@pytest.mark.asyncio
async def test_web_fetch_respects_max_length(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    long_body = "x" * 100

    class FakeResponse:
        status_code = 200
        headers = {"content-type": "text/plain"}

        @property
        def text(self) -> str:
            return long_body

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

    tool = WebFetchTool()
    result = await tool.execute(
        "tc2",
        {"url": "https://example.com", "max_length": 20},
    )
    assert len(result.content[0].text) > 20
    assert "truncated to 20 chars" in result.content[0].text
