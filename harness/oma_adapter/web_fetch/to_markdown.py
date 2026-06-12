"""HTML → markdown adapter (Node turndown parity via markdownify)."""

from __future__ import annotations

import re
from html.parser import HTMLParser

try:
    from bs4 import BeautifulSoup
    from markdownify import markdownify as _markdownify
except ImportError:  # pragma: no cover - dependency declared in pyproject
    BeautifulSoup = None  # type: ignore[misc, assignment]
    _markdownify = None


class _HtmlTextExtractor(HTMLParser):
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


def _fallback_html_to_text(html: str) -> str:
    parser = _HtmlTextExtractor()
    parser.feed(html)
    return parser.text()


def html_to_markdown(html: str, content_type: str = "text/html") -> str | None:
    """Convert HTML/XML/text bodies to markdown; None if unsupported."""
    mime = (content_type or "text/html").lower()
    if not (
        "html" in mime
        or "xml" in mime
        or mime.startswith("text/")
    ):
        return None
    if _markdownify is not None:
        try:
            html_input = html
            if BeautifulSoup is not None:
                soup = BeautifulSoup(html, "html.parser")
                for tag in soup(["script", "style", "nav", "footer", "header"]):
                    tag.decompose()
                html_input = str(soup)
            return _markdownify(
                html_input,
                heading_style="ATX",
            ).strip()
        except Exception:
            pass
    text = _fallback_html_to_text(html)
    return text if text else None
