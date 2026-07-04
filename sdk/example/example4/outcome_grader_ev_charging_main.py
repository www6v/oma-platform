#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
OG5 — EV fast-charging outcome grader live soak.

Full parity probe for Anthropic
``managed_agents/CMA_verify_with_outcome_grader.ipynb``: research writer
with web_search/web_fetch, strict seven-item rubric, grade-revise loop until
``satisfied`` or ``max_iterations_reached``.

Cookbook mapping
----------------
Cell 5   Agent + env + session  → web tools, unrestricted networking
Cell 7   define_outcome        → task.txt + rubric (file or inline)
Cell 9+  Stream loop          → stream_until_end_turn
Cell 14  Read brief.md        → Files API scope_id listing

This is a **live LLM soak** (minutes, real web fetches). Not run in CI.
Go CI covers platform mechanics via ``TestOutcomeGrader*``.

Usage::

    OMA_API_KEY=... OMA_BASE_URL=http://127.0.0.1:8787 \\
        python sdk/example/example4/outcome_grader_ev_charging.py

Optional:
  OMA_USE_INLINE_RUBRIC=1   inline text rubric (notebook style)
  OMA_EV_STRICT=1           exit non-zero unless result is ``satisfied``
  OMA_KEEP_RESOURCES=1      skip archive for Console inspection

Pytest (opt-in)::

    OMA_RUN_LIVE_OUTCOME_EV=1 pytest sdk/tests/test_outcome_cookbook.py -v -s
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

from ev_charging_soak import run_ev_charging_outcome_soak
from oma_sdk import OMAClient
from oma_sdk.api.agents import MODEL as DEFAULT_MODEL

OMA_BASE_URL = os.getenv("OMA_BASE_URL", "http://127.0.0.1:8787")
os.environ.setdefault("OMA_API_KEY", "dev-key")
MODEL = os.getenv("OMA_MODEL", DEFAULT_MODEL["id"])


async def main() -> None:
    use_file_rubric = os.getenv("OMA_USE_INLINE_RUBRIC") != "1"
    async with OMAClient(base_url=OMA_BASE_URL) as client:
        result = await run_ev_charging_outcome_soak(
            client,
            model=MODEL,
            use_file_rubric=use_file_rubric,
            keep_resources=os.getenv("OMA_KEEP_RESOURCES") == "1",
        )

    print("Session:", result["session_id"])
    print("Outcome passes:", result["outcome_pass_count"])
    print("Terminal result:", result["terminal_result"])
    print("brief.md in outputs:", result["has_brief"])

    terminal = result["terminal_result"]
    if terminal == "failed":
        raise SystemExit(f"outcome grader failed: {terminal!r}")
    if os.getenv("OMA_EV_STRICT") == "1" and terminal != "satisfied":
        raise SystemExit(f"OMA_EV_STRICT: expected satisfied, got {terminal!r}")
    if not result["has_brief"]:
        raise SystemExit("expected brief.md in session file outputs")


if __name__ == "__main__":
    asyncio.run(main())
