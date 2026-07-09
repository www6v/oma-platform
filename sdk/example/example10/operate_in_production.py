#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Operate in production — OMA Managed Agents cookbook example.

Mirrors ``CMA_operate_in_production.ipynb``: vault + GitHub MCP credential,
session vault_ids, MCP turn. Webhooks (§5) are documented, not executed.

Platform coverage (OP1–OP5): vault_ids + vault-scoped MCP — see CI
``TestOperateCookbook*`` and ``sdk/tests/test_operate_cookbook.py``.

Usage::

    GITHUB_TOKEN=... OMA_API_KEY=dev-key OMA_BASE_URL=http://127.0.0.1:8787 \\
        python sdk/example/example10/operate_in_production.py
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

from operate_in_production_soak import run_operate_in_production_soak
from oma_sdk import OMAClient
from oma_sdk.api.agents import MODEL as DEFAULT_MODEL

OMA_BASE_URL = os.getenv("OMA_BASE_URL", "http://127.0.0.1:8787")
GITHUB_MCP_URL = os.getenv(
    "GITHUB_MCP_URL",
    "https://api.githubcopilot.com/mcp/",
)
os.environ.setdefault("OMA_API_KEY", "dev-key")
MODEL = os.getenv("OMA_MODEL", DEFAULT_MODEL["id"])


async def main() -> None:
    token = os.getenv("GITHUB_TOKEN", "")
    if not token:
        sys.exit("GITHUB_TOKEN required for live operate soak")

    async with OMAClient(base_url=OMA_BASE_URL) as client:
        result = await run_operate_in_production_soak(
            client,
            model=MODEL,
            mcp_server_url=GITHUB_MCP_URL,
            github_token=token,
            keep_resources=os.getenv("OMA_KEEP_RESOURCES") == "1",
        )
    print("session:", result["session_id"])
    print("vault_id:", result["vault_id"])
    print("vault_ids:", result["vault_ids"])


if __name__ == "__main__":
    asyncio.run(main())
