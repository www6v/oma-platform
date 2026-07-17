"""OMA platform Python SDK availability check.

The OMA SDK (``oma_sdk``) is the supported way for the workflow bootstrap
to create agent/session records on the OMA platform. This module isolates
the import check so the rest of ``oma_adapter`` can ask
``is_oma_sdk_available()`` without sprinkling ``ImportError`` handling
around.
"""

from __future__ import annotations

import os


def is_oma_sdk_available() -> bool:
    """True when ``oma_sdk`` is importable AND OMA_API_KEY is set.

    The API key check matters: the harness loads ``.env`` after module
    import, so a bare ``ImportError`` guard would report True too early
    and the SDK client would later fail auth.
    """
    try:
        from oma_sdk import OMAClient  # noqa: F401
    except ImportError:
        return False
    return bool(os.environ.get("OMA_API_KEY", ""))
