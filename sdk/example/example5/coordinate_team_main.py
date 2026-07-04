#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Coordinate specialist team — OMA Managed Agents cookbook example.

Quick live demo (same harness path as CT6 soak, lighter exit checks).
For the full CT6 probe with strict assertions, use ``coordinate_team_live.py``.

Usage::

    OMA_API_KEY=dev-key OMA_BASE_URL=http://127.0.0.1:8787 \\
        python sdk/example/example5/coordinate_team.py
"""
from __future__ import annotations

import sys

if sys.version_info < (3, 11):
    sys.exit(
        "Python 3.11+ required (found {}). Run: python3 {}".format(
            sys.version.split()[0],
            sys.argv[0],
        )
    )

import asyncio
import os
from pathlib import Path

_EXAMPLE_DIR = Path(__file__).resolve().parent
if str(_EXAMPLE_DIR) not in sys.path:
    sys.path.insert(0, str(_EXAMPLE_DIR))

from coordinate_team_soak import run_coordinate_team_soak
from oma_sdk import OMAClient
from oma_sdk.api.agents import MODEL as DEFAULT_MODEL

OMA_BASE_URL = os.getenv("OMA_BASE_URL", "http://127.0.0.1:8787")
os.environ.setdefault("OMA_API_KEY", "dev-key")
MODEL = os.getenv("OMA_MODEL", DEFAULT_MODEL["id"])


async def main() -> None:
    async with OMAClient(base_url=OMA_BASE_URL) as client:
        result = await run_coordinate_team_soak(
            client,
            model=MODEL,
            keep_resources=os.getenv("OMA_KEEP_RESOURCES") == "1",
        )

    print("Session:", result["session_id"])
    print("thread_created:", result["thread_created_count"])
    print("thread_message_received:", result["thread_received_count"])
    print("proposal.md in outputs:", result["has_proposal"])


if __name__ == "__main__":
    asyncio.run(main())
