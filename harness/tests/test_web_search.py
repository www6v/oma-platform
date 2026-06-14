from __future__ import annotations

import json
from typing import Any

import pytest

from oma_adapter.extensions.web_search import WebSearchTool
from oma_adapter.web_search.core import search_duckduckgo, search_tavily, search_web
from oma_adapter.web_search.runtime import (
    WebSearchRuntime,
    clear_web_search_runtime,
    configure_web_search,
    resolve_search_backend,
)
from oma_adapter.types import AgentSnapshot


VQD_HTML = "<html>vqd='12-34-56'</html>"
DDG_BODY = (
    "DDG.pageLayout.load('d',["
    "{\"t\":\"Result\",\"u\":\"https://example.com\",\"a\":\"<b>desc</b>\"}"
    "]);DDG.duckbar.load"
)


@pytest.mark.asyncio
async def test_search_duckduckgo_parses_results(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    calls: list[str] = []

    async def fake_fetch(url: str, runtime: WebSearchRuntime) -> tuple[str, str | None]:
        del runtime
        calls.append(url)
        if "duckduckgo.com/?" in url:
            return VQD_HTML, None
        return DDG_BODY, None

    monkeypatch.setattr(
        "oma_adapter.web_search.core._fetch_text",
        fake_fetch,
    )
    configure_web_search(WebSearchRuntime())
    try:
        raw = await search_duckduckgo("oma test", 5, get_runtime())
        data = json.loads(raw)
        assert len(data) == 1
        assert data[0]["title"] == "Result"
        assert data[0]["url"] == "https://example.com"
        assert data[0]["description"] == "desc"
        assert len(calls) == 2
    finally:
        clear_web_search_runtime()


def get_runtime() -> WebSearchRuntime:
    from oma_adapter.web_search.runtime import get_web_search_runtime

    return get_web_search_runtime()


@pytest.mark.asyncio
async def test_search_duckduckgo_rate_limited(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    async def fake_fetch(url: str, runtime: WebSearchRuntime) -> tuple[str, str | None]:
        del runtime
        if "duckduckgo.com/?" in url:
            return VQD_HTML, None
        return "DDG.deep.anomalyDetectionBlock", None

    monkeypatch.setattr(
        "oma_adapter.web_search.core._fetch_text",
        fake_fetch,
    )
    configure_web_search(WebSearchRuntime())
    try:
        text = await search_duckduckgo("test", 5, get_runtime())
        assert "rate limited" in text.lower()
    finally:
        clear_web_search_runtime()


@pytest.mark.asyncio
async def test_search_tavily_maps_results(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    class FakeResponse:
        def json(self) -> dict[str, Any]:
            return {
                "results": [
                    {
                        "title": "Tavily hit",
                        "url": "https://tavily.test/a",
                        "content": "snippet text",
                    },
                ],
            }

    class FakeClient:
        def __init__(self, *args: Any, **kwargs: Any) -> None:
            del args, kwargs

        async def __aenter__(self) -> FakeClient:
            return self

        async def __aexit__(self, *args: Any) -> None:
            del args

        async def post(self, url: str, **kwargs: Any) -> FakeResponse:
            del url, kwargs
            return FakeResponse()

    monkeypatch.setattr(
        "oma_adapter.web_search.core.httpx.AsyncClient",
        FakeClient,
    )
    configure_web_search(WebSearchRuntime())
    try:
        raw = await search_tavily("query", 3, "tvly-key", get_runtime())
        data = json.loads(raw)
        assert data[0]["title"] == "Tavily hit"
        assert data[0]["snippet"] == "snippet text"
    finally:
        clear_web_search_runtime()


@pytest.mark.asyncio
async def test_search_web_uses_tavily_when_configured() -> None:
    configure_web_search(
        WebSearchRuntime(backend="tavily", tavily_api_key=None),
    )
    try:
        text = await search_web("q", 5)
        assert "TAVILY_API_KEY not configured" in text
    finally:
        clear_web_search_runtime()


def test_resolve_search_backend_tavily_type() -> None:
    agent = AgentSnapshot(
        id="a",
        name="n",
        model="m",
        tools=[{"type": "web_search_tavily"}],
    )
    assert resolve_search_backend(agent) == "tavily"


def test_resolve_search_backend_default_ddg() -> None:
    agent = AgentSnapshot(id="a", name="n", model="m")
    assert resolve_search_backend(agent) == "ddg"


@pytest.mark.asyncio
async def test_web_search_tool_requires_query() -> None:
    tool = WebSearchTool()
    result = await tool.execute("tc1", {}, None, None)
    assert result.is_error
    assert "query is required" in result.content[0].text


@pytest.mark.asyncio
async def test_web_search_tool_executes(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    async def fake_search(query: str, max_results: int | None = None) -> str:
        del max_results
        return json.dumps([{"title": "Hit", "url": "https://x.test", "description": ""}])

    monkeypatch.setattr(
        "oma_adapter.extensions.web_search.search_web",
        fake_search,
    )
    tool = WebSearchTool()
    result = await tool.execute("tc1", {"query": "hello"}, None, None)
    assert not result.is_error
    assert "Hit" in result.content[0].text
