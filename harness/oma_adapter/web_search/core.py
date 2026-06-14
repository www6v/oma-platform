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
USER_AGENT = (
    "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) "
    "AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
)
DEFAULT_MAX_RESULTS = 5
MAX_RESULTS_CAP = 20


def _search_host_allowed(hostname: str, environment: dict[str, Any] | None) -> bool:
    return _host_allowed(hostname, environment)


async def _http_get(url: str, runtime: WebSearchRuntime) -> tuple[str, str | None]:
    """Fetch directly (no outbound vault proxy — see module doc on d.js path)."""
    del runtime
    headers: dict[str, str] = {"User-Agent": USER_AGENT}

    async with httpx.AsyncClient(
        follow_redirects=True,
        timeout=SEARCH_TIMEOUT_SEC,
    ) as client:
        response = await client.get(url, headers=headers)
        if response.status_code >= 400:
            return "", f"HTTP {response.status_code}"
        return response.text, None


async def _http_post_form(
    url: str,
    form: dict[str, str],
    runtime: WebSearchRuntime,
) -> tuple[str, str | None]:
    del runtime
    headers: dict[str, str] = {"User-Agent": USER_AGENT}

    async with httpx.AsyncClient(
        follow_redirects=True,
        timeout=SEARCH_TIMEOUT_SEC,
    ) as client:
        response = await client.post(url, data=form, headers=headers)
        if response.status_code >= 400:
            return "", f"HTTP {response.status_code}"
        return response.text, None


def _append_result(
    results: list[dict[str, str]],
    seen: set[str],
    title: str,
    url: str,
    description: str,
    max_results: int,
) -> None:
    if not url or url in seen or len(results) >= max_results:
        return
    seen.add(url)
    results.append(
        {
            "title": title,
            "url": url,
            "description": description,
        }
    )


def _collect_instant_topics(
    topics: Any,
    results: list[dict[str, str]],
    seen: set[str],
    max_results: int,
) -> None:
    if not isinstance(topics, list):
        return
    for topic in topics:
        if not isinstance(topic, dict):
            continue
        nested = topic.get("Topics")
        if isinstance(nested, list):
            _collect_instant_topics(nested, results, seen, max_results)
            continue
        first_url = topic.get("FirstURL")
        if not isinstance(first_url, str) or not first_url:
            continue
        text = str(topic.get("Text") or first_url)
        title = text.split(" - ", 1)[0].strip() or first_url
        _append_result(results, seen, title, first_url, text, max_results)


def _parse_instant_answer(
    data: dict[str, Any],
    query: str,
    max_results: int,
) -> list[dict[str, str]]:
    results: list[dict[str, str]] = []
    seen: set[str] = set()

    abstract_url = data.get("AbstractURL")
    if isinstance(abstract_url, str) and abstract_url:
        heading = str(data.get("Heading") or query)
        abstract = str(data.get("AbstractText") or "")
        _append_result(results, seen, heading, abstract_url, abstract, max_results)

    for row in data.get("Results") or []:
        if not isinstance(row, dict):
            continue
        first_url = row.get("FirstURL")
        if not isinstance(first_url, str) or not first_url:
            continue
        text = str(row.get("Text") or first_url)
        title = text.split(" - ", 1)[0].strip() or first_url
        _append_result(results, seen, title, first_url, text, max_results)

    _collect_instant_topics(
        data.get("RelatedTopics") or [],
        results,
        seen,
        max_results,
    )
    return results


async def _search_duckduckgo_djs(
    query: str,
    max_results: int,
    runtime: WebSearchRuntime,
) -> list[dict[str, str]]:
    vqd_url = (
        f"https://duckduckgo.com/?"
        f"{urlencode({'q': query, 'ia': 'web'})}"
    )
    vqd_text, err = await _http_get(vqd_url, runtime)
    if err is not None:
        return []

    vqd_match = re.search(r"vqd=['\"](\d+-\d+(?:-\d+)?)['\"]", vqd_text)
    if not vqd_match:
        return []

    params = {
        "q": query,
        "l": "en-us",
        "kl": "wt-wt",
        "s": "0",
        "dl": "en",
        "ct": "US",
        "ss_mkt": "us",
        "vqd": vqd_match.group(1),
        "sp": "1",
        "bpa": "1",
    }
    search_url = f"https://links.duckduckgo.com/d.js?{urlencode(params)}"
    if not _search_host_allowed("duckduckgo.com", runtime.environment):
        return []

    body, search_err = await _http_get(search_url, runtime)
    if search_err is not None or "DDG.deep.anomalyDetectionBlock" in body:
        return []

    match = re.search(
        r"DDG\.pageLayout\.load\('d',(\[.+?\])\);DDG\.duckbar\.load",
        body,
    )
    if not match:
        return []

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
    return results


async def _search_duckduckgo_instant(
    query: str,
    max_results: int,
    runtime: WebSearchRuntime,
) -> list[dict[str, str]]:
    if not _search_host_allowed("duckduckgo.com", runtime.environment):
        return []

    api_url = (
        "https://api.duckduckgo.com/?"
        f"{urlencode({'q': query, 'format': 'json', 'no_redirect': '1'})}"
    )
    body, err = await _http_get(api_url, runtime)
    if err is not None:
        return []
    try:
        data = json.loads(body)
    except json.JSONDecodeError:
        return []
    if not isinstance(data, dict):
        return []
    return _parse_instant_answer(data, query, max_results)


async def _search_duckduckgo_lite(
    query: str,
    max_results: int,
    runtime: WebSearchRuntime,
) -> list[dict[str, str]]:
    if not _search_host_allowed("duckduckgo.com", runtime.environment):
        return []

    body, err = await _http_post_form(
        "https://lite.duckduckgo.com/lite/",
        {"q": query},
        runtime,
    )
    if err is not None:
        return []

    results: list[dict[str, str]] = []
    seen: set[str] = set()
    for url, title in re.findall(
        r'<a rel="nofollow" href="(https?://[^"]+)">([^<]+)</a>',
        body,
    ):
        _append_result(
            results,
            seen,
            title.strip(),
            url.strip(),
            "",
            max_results,
        )
    return results


async def search_duckduckgo(
    query: str,
    max_results: int,
    runtime: WebSearchRuntime,
) -> str:
    if not _search_host_allowed("duckduckgo.com", runtime.environment):
        return (
            "Error: Host \"duckduckgo.com\" is not allowed in limited networking"
        )

    for backend in (
        _search_duckduckgo_djs,
        _search_duckduckgo_instant,
        _search_duckduckgo_lite,
    ):
        results = await backend(query, max_results, runtime)
        if results:
            return json.dumps(results)

    return "DuckDuckGo: no results found"


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

    del runtime
    headers: dict[str, str] = {
        "Content-Type": "application/json",
        "User-Agent": USER_AGENT,
    }

    async with httpx.AsyncClient(
        follow_redirects=True,
        timeout=SEARCH_TIMEOUT_SEC,
    ) as client:
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
