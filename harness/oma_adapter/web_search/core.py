"""web_search: DuckDuckGo (default) or Tavily API."""

from __future__ import annotations

import json
import os
import re
from typing import Any
from urllib.parse import urlencode

import httpx

from oma_adapter.web_fetch.core import _host_allowed
from oma_adapter.web_search.runtime import WebSearchRuntime, get_web_search_runtime

SEARCH_TIMEOUT_SEC = 20.0
USER_AGENT = "OMA-Agent/1.0 (+web_search)"
DEFAULT_MAX_RESULTS = 5
MAX_RESULTS_CAP = 20


def _search_host_allowed(hostname: str, environment: dict[str, Any] | None) -> bool:
    return _host_allowed(hostname, environment)


async def _fetch_text(url: str, runtime: WebSearchRuntime) -> tuple[str, str | None]:
    headers: dict[str, str] = {"User-Agent": USER_AGENT}
    if runtime.session_id:
        headers["X-OMA-Session-Id"] = runtime.session_id
    if runtime.outbound_proxy_api_key:
        headers["Proxy-Authorization"] = (
            f"Bearer {runtime.outbound_proxy_api_key}"
        )

    client_kwargs: dict[str, Any] = {
        "follow_redirects": True,
        "timeout": SEARCH_TIMEOUT_SEC,
    }
    if runtime.outbound_proxy_url:
        client_kwargs["proxy"] = runtime.outbound_proxy_url

    async with httpx.AsyncClient(**client_kwargs) as client:
        response = await client.get(url, headers=headers)
        if response.status_code >= 400:
            return "", f"HTTP {response.status_code}"
        return response.text, None


async def search_duckduckgo(
    query: str,
    max_results: int,
    runtime: WebSearchRuntime,
) -> str:
    if not _search_host_allowed("duckduckgo.com", runtime.environment):
        return (
            "Error: Host \"duckduckgo.com\" is not allowed in limited networking"
        )

    vqd_url = (
        f"https://duckduckgo.com/?"
        f"{urlencode({'q': query, 'ia': 'web'})}"
    )
    vqd_text, err = await _fetch_text(vqd_url, runtime)
    if err is not None:
        return f"DuckDuckGo error: {err}"

    vqd_match = re.search(r"vqd=['\"](\d+-\d+(?:-\d+)?)['\"]", vqd_text)
    if not vqd_match:
        return "DuckDuckGo: failed to get search token"
    vqd = vqd_match.group(1)

    params = {
        "q": query,
        "l": "en-us",
        "kl": "wt-wt",
        "s": "0",
        "dl": "en",
        "ct": "US",
        "ss_mkt": "us",
        "vqd": vqd,
        "sp": "1",
        "bpa": "1",
    }
    search_url = f"https://links.duckduckgo.com/d.js?{urlencode(params)}"
    if not _search_host_allowed("links.duckduckgo.com", runtime.environment):
        return (
            "Error: Host \"links.duckduckgo.com\" is not allowed "
            "in limited networking"
        )

    body, search_err = await _fetch_text(search_url, runtime)
    if search_err is not None:
        return f"DuckDuckGo search error: {search_err}"

    if "DDG.deep.anomalyDetectionBlock" in body:
        return "DuckDuckGo rate limited. Try again in a moment."

    match = re.search(
        r"DDG\.pageLayout\.load\('d',(\[.+?\])\);DDG\.duckbar\.load",
        body,
    )
    if not match:
        return "DuckDuckGo: no results found"

    raw = json.loads(match.group(1).replace("\t", "    "))
    results: list[dict[str, str]] = []
    for row in raw:
        if row.get("u") and "n" not in row:
            desc = str(row.get("a") or "")
            desc = desc.replace("</b>", "").replace("<b>", "")
            results.append(
                {
                    "title": str(row.get("t") or ""),
                    "url": str(row.get("u") or ""),
                    "description": desc,
                }
            )
            if len(results) >= max_results:
                break

    return json.dumps(results)


async def search_tavily(
    query: str,
    max_results: int,
    api_key: str,
    runtime: WebSearchRuntime,
) -> str:
    if not _search_host_allowed("api.tavily.com", runtime.environment):
        return (
            "Error: Host \"api.tavily.com\" is not allowed in limited networking"
        )

    headers: dict[str, str] = {
        "Content-Type": "application/json",
        "User-Agent": USER_AGENT,
    }
    if runtime.session_id:
        headers["X-OMA-Session-Id"] = runtime.session_id
    if runtime.outbound_proxy_api_key:
        headers["Proxy-Authorization"] = (
            f"Bearer {runtime.outbound_proxy_api_key}"
        )

    client_kwargs: dict[str, Any] = {
        "follow_redirects": True,
        "timeout": SEARCH_TIMEOUT_SEC,
    }
    if runtime.outbound_proxy_url:
        client_kwargs["proxy"] = runtime.outbound_proxy_url

    async with httpx.AsyncClient(**client_kwargs) as client:
        response = await client.post(
            "https://api.tavily.com/search",
            headers=headers,
            json={
                "api_key": api_key,
                "query": query,
                "max_results": max_results,
            },
        )
        data = response.json()

    results = data.get("results") or []
    mapped = [
        {
            "title": row.get("title"),
            "url": row.get("url"),
            "snippet": row.get("content"),
        }
        for row in results
        if isinstance(row, dict)
    ]
    if mapped:
        return json.dumps(mapped)
    return json.dumps(data)


async def search_web(query: str, max_results: int | None = None) -> str:
    runtime = get_web_search_runtime()
    count = max_results if max_results is not None else DEFAULT_MAX_RESULTS
    count = max(1, min(int(count), MAX_RESULTS_CAP))

    if runtime.backend == "tavily":
        api_key = runtime.tavily_api_key or os.environ.get("TAVILY_API_KEY")
        if not api_key:
            return "web_search unavailable: TAVILY_API_KEY not configured"
        return await search_tavily(query, count, api_key, runtime)

    return await search_duckduckgo(query, count, runtime)
