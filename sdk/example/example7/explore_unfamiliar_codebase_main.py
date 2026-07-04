#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Explore unfamiliar codebase — OMA Managed Agents cookbook example.

Mirrors ``CMA_explore_unfamiliar_codebase.ipynb``: mount repo.zip, explore
turns, mid-session ``sessions.resources.add`` / delete.

Platform coverage (EX1–EX4): zip resolve + mid-session resources — see CI
``TestExploreCookbook*``.

Usage::

    OMA_API_KEY=dev-key OMA_BASE_URL=http://127.0.0.1:8787 \\
        python sdk/example/example7/explore_unfamiliar_codebase.py
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

from explore_unfamiliar_codebase_soak import run_explore_unfamiliar_codebase_soak
from oma_sdk import OMAClient
from oma_sdk.api.agents import MODEL as DEFAULT_MODEL

OMA_BASE_URL = os.getenv("OMA_BASE_URL", "http://127.0.0.1:8787")
os.environ.setdefault("OMA_API_KEY", "dev-key")
MODEL = os.getenv("OMA_MODEL", DEFAULT_MODEL["id"])


async def main() -> None:
    async with OMAClient(base_url=OMA_BASE_URL) as client:
        result = await run_explore_unfamiliar_codebase_soak(
            client,
            model=MODEL,
            keep_resources=os.getenv("OMA_KEEP_RESOURCES") == "1",
        )
    print("session:", result["session_id"])
    print("resources after add:", result["resource_count_after_add"])
    print("resources after delete:", result["resource_count_after_delete"])
    if result["resource_count_after_add"] < 2:
        raise SystemExit("expected 2 resources after mid-session add")
    if result["resource_count_after_delete"] != 1:
        raise SystemExit("expected 1 resource after detach")


if __name__ == "__main__":
    asyncio.run(main())
