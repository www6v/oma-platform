"""OMA web_fetch helpers (toMarkdown, aux summarize, .web offload)."""

from oma_adapter.web_fetch.core import fetch_web_content
from oma_adapter.web_fetch.runtime import (
    WebFetchRuntime,
    clear_web_fetch_runtime,
    configure_web_fetch,
    get_web_fetch_runtime,
)

__all__ = [
    "WebFetchRuntime",
    "clear_web_fetch_runtime",
    "configure_web_fetch",
    "fetch_web_content",
    "get_web_fetch_runtime",
]
