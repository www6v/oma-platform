"""web_search runtime helpers."""

from oma_adapter.web_search.runtime import (
    clear_web_search_runtime,
    configure_web_search,
    get_web_search_runtime,
    resolve_search_backend,
)

__all__ = [
    "clear_web_search_runtime",
    "configure_web_search",
    "get_web_search_runtime",
    "resolve_search_backend",
]
