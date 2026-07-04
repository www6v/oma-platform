#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
CT6 — Coordinate specialist team live soak.

Full parity probe for Anthropic
``managed_agents/CMA_coordinate_specialist_team.ipynb``: three specialists
(prospect_researcher with web_search, case_study_picker, pricing_modeler)
wired via ``multiagent``, session resources under ``/mnt/user-data/``, and
delegation thread events ending in ``proposal.md``.

This is a **live LLM soak** (minutes, real delegation). Not run in CI.
Go CI covers platform mechanics via ``TestCoordinateCookbook*``.

Usage::

    OMA_API_KEY=... OMA_BASE_URL=http://127.0.0.1:8787 \\
        python sdk/example/example5/coordinate_team_live.py

Optional:
  OMA_COORDINATE_STRICT=1   require 3 threads + web_search + proposal.md
  OMA_KEEP_RESOURCES=1      skip archive for Console inspection
  OMA_COORDINATE_TIMEOUT_SEC=1800

Pytest (opt-in)::

    OMA_RUN_LIVE_COORDINATE=1 pytest sdk/tests/test_coordinate_cookbook.py -v -s
"""
from __future__ import annotations

import asyncio
import os
import sys
from pathlib import Path

_EXAMPLE_DIR = Path(__file__).resolve().parent
if str(_EXAMPLE_DIR) not in sys.path:
    sys.path.insert(0, str(_EXAMPLE_DIR))

if sys.version_info < (3, 11):
    sys.exit(
        "Python 3.11+ required (found {}). Run: python3 {}".format(
            sys.version.split()[0],
            sys.argv[0],
        )
    )

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
    print("threads:", result["threads_count"])
    print("call_agent uses:", result["call_agent_uses"])
    print("web_search used:", result["saw_web_search"])
    print("proposal.md in outputs:", result["has_proposal"])

    if result["thread_created_count"] < 1:
        raise SystemExit("expected at least one session.thread_created (delegation)")
    if not result["has_proposal"]:
        raise SystemExit("expected proposal.md in session file outputs")

    if os.getenv("OMA_COORDINATE_STRICT") == "1":
        if result["thread_created_count"] < 3:
            raise SystemExit(
                f"OMA_COORDINATE_STRICT: thread_created={result['thread_created_count']} want >=3",
            )
        if result["thread_received_count"] < 3:
            raise SystemExit(
                "OMA_COORDINATE_STRICT: expected 3 thread_message_received events",
            )
        if not result["saw_web_search"]:
            raise SystemExit(
                "OMA_COORDINATE_STRICT: expected web_search on researcher thread",
            )


if __name__ == "__main__":
    asyncio.run(main())
