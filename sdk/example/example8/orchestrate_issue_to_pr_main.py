#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Orchestrate issue to PR — OMA Managed Agents cookbook example.

Mirrors ``CMA_orchestrate_issue_to_pr.ipynb``: mock ``gh-mock`` zip fixture,
multi-turn issue→PR chain with CI/review recovery, verification turn.

Platform coverage (OR1–OR4): zip mount + env pytest + multi-turn — see CI
``TestOrchestrateCookbook*``.

Usage::

    OMA_API_KEY=dev-key OMA_BASE_URL=http://127.0.0.1:8787 \\
        python sdk/example/example8/orchestrate_issue_to_pr.py
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

from orchestrate_issue_to_pr_soak import run_orchestrate_issue_to_pr_soak
from oma_sdk import OMAClient
from oma_sdk.api.agents import MODEL as DEFAULT_MODEL

OMA_BASE_URL = os.getenv("OMA_BASE_URL", "http://127.0.0.1:8787")
os.environ.setdefault("OMA_API_KEY", "dev-key")
MODEL = os.getenv("OMA_MODEL", DEFAULT_MODEL["id"])


async def main() -> None:
    async with OMAClient(base_url=OMA_BASE_URL) as client:
        result = await run_orchestrate_issue_to_pr_soak(
            client,
            model=MODEL,
            keep_resources=os.getenv("OMA_KEEP_RESOURCES") == "1",
        )
    print("session:", result["session_id"])
    print("turn_count:", result["turn_count"])


if __name__ == "__main__":
    asyncio.run(main())
