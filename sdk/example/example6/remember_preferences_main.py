#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Remember user preferences — OMA Managed Agents cookbook example.

Mirrors ``CMA_remember_user_preferences.ipynb``: attach a memory_store
resource, save a preference in session 1, recall it in session 2.

Platform coverage (RP1–RP3): live memory resolve + workdir sync + cross-session
recall — see CI ``TestRememberCookbook*``.

Usage::

    OMA_API_KEY=dev-key OMA_BASE_URL=http://127.0.0.1:8787 \\
        python sdk/example/example6/remember_preferences.py
"""
from __future__ import annotations

import asyncio
import os
import sys

if sys.version_info < (3, 11):
    sys.exit("Python 3.11+ required")

_EXAMPLE_DIR = __import__("pathlib").Path(__file__).resolve().parent
if str(_EXAMPLE_DIR) not in sys.path:
    sys.path.insert(0, str(_EXAMPLE_DIR))

from remember_preferences_soak import run_remember_preferences_soak
from oma_sdk import OMAClient
from oma_sdk.api.agents import MODEL as DEFAULT_MODEL

OMA_BASE_URL = os.getenv("OMA_BASE_URL", "http://127.0.0.1:8787")
os.environ.setdefault("OMA_API_KEY", "dev-key")
MODEL = os.getenv("OMA_MODEL", DEFAULT_MODEL["id"])


async def main() -> None:
    async with OMAClient(base_url=OMA_BASE_URL) as client:
        result = await run_remember_preferences_soak(
            client,
            model=MODEL,
            keep_resources=os.getenv("OMA_KEEP_RESOURCES") == "1",
        )
    print("memory_store:", result["memory_store_id"])
    print("session save:", result["session_save_id"])
    print("session recall:", result["session_recall_id"])
    print("saved:", bool(result["saved_content"]))
    print("cross_session_recall:", result["cross_session_recall"])
    if not result["saved_content"]:
        raise SystemExit("expected preference persisted after session 1")


if __name__ == "__main__":
    asyncio.run(main())
