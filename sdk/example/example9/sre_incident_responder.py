#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
SRE incident responder — OMA Managed Agents cookbook example.

Mirrors ``sre_incident_responder.ipynb``: incident-runbooks skill, three file
mounts (logs, manifest, runbook), PagerDuty alert, and HITL ``request_approval``.

Platform coverage (SRE1–SRE4): skill inject + resources + custom tools HITL —
see CI ``TestSreCookbook*`` and ``sdk/tests/test_sre_cookbook.py``.

Usage::

    OMA_API_KEY=dev-key OMA_BASE_URL=http://127.0.0.1:8787 \\
        python sdk/example/example9/sre_incident_responder.py
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

from sre_incident_responder_soak import run_sre_incident_responder_soak
from oma_sdk import OMAClient
from oma_sdk.api.agents import MODEL as DEFAULT_MODEL

OMA_BASE_URL = os.getenv("OMA_BASE_URL", "http://127.0.0.1:8787")
os.environ.setdefault("OMA_API_KEY", "dev-key")
MODEL = os.getenv("OMA_MODEL", DEFAULT_MODEL["id"])


async def main() -> None:
    async with OMAClient(base_url=OMA_BASE_URL) as client:
        result = await run_sre_incident_responder_soak(
            client,
            model=MODEL,
            keep_resources=os.getenv("OMA_KEEP_RESOURCES") == "1",
        )
    print("session:", result["session_id"])
    print("pr_state:", result["pr_state"])
    print("responded_ids:", result["responded_ids"])


if __name__ == "__main__":
    asyncio.run(main())
