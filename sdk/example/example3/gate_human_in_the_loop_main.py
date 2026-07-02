#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Gate: human-in-the-loop with custom tools — OMA Managed Agents cookbook example.

Mirrors Anthropic cookbook ``managed_agents/CMA_gate_human_in_the_loop.ipynb``:
expense approver with ``decide()`` / ``escalate()`` custom tools that round-trip
through the application.

Cookbook section → OMA function mapping
---------------------------------------
Cell 1   Setup              → OMAClient, FIXTURE dir
§1       Upload policy + receipts → client.files.upload
§2       Agent + env + session    → custom tools + resources[]
Part A   HITL stream loop         → stream_hitl_until_end_turn()
Part B   Webhooks                 → documented only (operate notebook)

Platform coverage (GT1–GT4): custom-tool registration, ``agent.custom_tool_use``,
``requires_action`` idle, ``user.custom_tool_result`` resume — see CI
``TestGateCookbook*`` and ``sdk/tests/test_gate_cookbook.py``.

Prerequisites
-------------
* Python 3.11+
* ``oma_sdk`` on PYTHONPATH or ``pip install -e sdk`` from oma-platform
* OMA server + harness with a real LLM for live runs

Usage::

    OMA_API_KEY=dev-key OMA_BASE_URL=http://127.0.0.1:8787 \\
        python sdk/example/example3/gate_human_in_the_loop.py
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
import io
import json
import os
from collections import Counter
from pathlib import Path
from typing import Any

from oma_sdk import (
    OMAClient,
    StreamConfig,
    print_stream_event,
    stream_hitl_until_end_turn,
    wait_for_idle_status,
)
from oma_sdk.api.agents import MODEL as DEFAULT_MODEL

OMA_BASE_URL = os.getenv("OMA_BASE_URL", "http://127.0.0.1:8787")
os.environ.setdefault("OMA_API_KEY", "dev-key")

ENV_NAME = "cookbook-gate-env"
AGENT_NAME = "cookbook-gate"
SESSION_TITLE = "Expense gate"
MODEL = os.getenv("OMA_MODEL", DEFAULT_MODEL["id"])

SCRIPT_DIR = Path(__file__).resolve().parent
FIXTURE_DIR = SCRIPT_DIR / "gate"
POLICY_PATH = FIXTURE_DIR / "policy.yaml"
RECEIPTS_PATH = FIXTURE_DIR / "inbox" / "receipts.jsonl"

POLICY_MOUNT = "policy.yaml"
RECEIPTS_MOUNT = "receipts.jsonl"

GATE_SYSTEM_PROMPT = (
    "You are an expense approver. Read each receipt in "
    "receipts.jsonl against the policy in policy.yaml and make "
    "exactly ONE tool call per receipt. Call decide(receipt_id, "
    "action, reason) for clear cases, or escalate(receipt_id, "
    "question) for ambiguous ones (near thresholds, unclear "
    "categories, suspicious notes). Once you've called decide "
    "or escalate for a given receipt, that receipt is finalized "
    "— do not call either tool for it again. After processing "
    "all receipts exactly once, stop."
)

# Cookbook §2: agent_toolset + decide + escalate custom tools.
GATE_TOOLS = [
    {
        "type": "agent_toolset_20260401",
        "default_config": {
            "enabled": True,
            "permission_policy": {"type": "always_allow"},
        },
    },
    {
        "type": "custom",
        "name": "decide",
        "description": "Record a final approve/reject for a clear-cut receipt.",
        "input_schema": {
            "type": "object",
            "properties": {
                "receipt_id": {"type": "string"},
                "action": {
                    "type": "string",
                    "enum": ["approve", "reject"],
                },
                "reason": {"type": "string"},
            },
            "required": ["receipt_id", "action", "reason"],
        },
    },
    {
        "type": "custom",
        "name": "escalate",
        "description": "Surface an ambiguous receipt for human review.",
        "input_schema": {
            "type": "object",
            "properties": {
                "receipt_id": {"type": "string"},
                "question": {"type": "string"},
            },
            "required": ["receipt_id", "question"],
        },
    },
]

GATE_TASK = (
    "Read /mnt/session/uploads/policy.yaml and "
    "/mnt/session/uploads/receipts.jsonl. Process "
    "all 12 receipts. For each receipt, make "
    "exactly one decide() or escalate() call and "
    "then move on to the next. When every receipt "
    "has been processed once, stop."
)


def _load_dotenv() -> None:
    dotenv = Path(__file__).resolve().parents[2] / ".env"
    if not dotenv.exists():
        return
    for line in dotenv.read_text().splitlines():
        line = line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        key, value = line.split("=", 1)
        os.environ.setdefault(key.strip(), value.strip())


def _stream_config() -> StreamConfig:
    return StreamConfig(
        timeout_sec=float(os.getenv("OMA_DEMO_TIMEOUT_SEC", "900")),
        stream_read_timeout=float(
            os.getenv("OMA_STREAM_READ_TIMEOUT_SEC", "300"),
        ),
        idle_poll_max_wait=float(
            os.getenv("OMA_IDLE_POLL_MAX_WAIT_SEC", "30"),
        ),
    )


def simulate_human_review(receipt_id: str, question: str) -> str:
    """Cookbook Part A: inline reviewer for escalate() calls."""
    del receipt_id
    return "reject" if "suspicious" in question.lower() else "approve"


async def upload_fixture(
    client: OMAClient,
    path: Path,
    mime: str,
) -> dict:
    payload = path.read_bytes()
    return await client.files.upload(
        file=(path.name, io.BytesIO(payload), mime),
        downloadable=True,
    )


def make_gate_handler(
    decisions: dict[str, dict[str, Any]],
) -> Any:
    """Build on_custom_tool callback matching cookbook Part A."""

    def handle_custom_tool(
        name: str,
        _event_id: str,
        args: dict[str, Any],
    ) -> dict[str, Any]:
        receipt_id = str(args.get("receipt_id") or "")
        if name == "decide":
            decisions[receipt_id] = {"lane": args.get("action"), **args}
            return {"recorded": True}
        if name == "escalate":
            question = str(args.get("question") or "")
            human = simulate_human_review(receipt_id, question)
            decisions[receipt_id] = {
                "lane": "escalated",
                "human_decision": human,
                **args,
            }
            return {"human_decision": human}
        return {"error": f"unknown tool {name}"}

    return handle_custom_tool


async def main() -> None:
    _load_dotenv()

    print(f"OMA base URL : {OMA_BASE_URL}")
    print(f"Fixtures     : {FIXTURE_DIR}  (exists: {FIXTURE_DIR.is_dir()})")
    if not POLICY_PATH.is_file() or not RECEIPTS_PATH.is_file():
        raise FileNotFoundError(f"Missing gate fixtures under {FIXTURE_DIR}")

    client = OMAClient(base_url=OMA_BASE_URL)
    decisions: dict[str, dict[str, Any]] = {}

    try:
        # §1 Upload policy and receipts — Cookbook Cell 3
        policy_file = await upload_fixture(client, POLICY_PATH, "text/yaml")
        receipts_file = await upload_fixture(
            client,
            RECEIPTS_PATH,
            "application/jsonl",
        )
        print(f"uploaded: {policy_file['id']}, {receipts_file['id']}")

        # §2 Agent + environment + session — Cookbook Cell 5
        agent = client.agents.create(
            name=AGENT_NAME,
            model={"id": MODEL},
            system=GATE_SYSTEM_PROMPT,
            tools=GATE_TOOLS,
        )
        print(f"Created agent: id={agent.id}  version={agent.version}")

        env = client.environments.create(
            name=ENV_NAME,
            config={"type": "cloud", "networking": {"type": "limited"}},
        )
        print(f"Created environment: {env.id}")

        session = client.sessions.create(
            environment_id=env.id,
            agent={"type": "agent", "id": agent.id, "version": agent.version},
            resources=[
                {
                    "type": "file",
                    "file_id": policy_file["id"],
                    "mount_path": POLICY_MOUNT,
                },
                {
                    "type": "file",
                    "file_id": receipts_file["id"],
                    "mount_path": RECEIPTS_MOUNT,
                },
            ],
            title=SESSION_TITLE,
        )
        print(f"session: {session.id}")

        resources = getattr(session, "resources", None) or []
        if len(resources) < 2:
            raise RuntimeError(
                "session.resources missing mounted policy/receipts files"
            )

        # Part A — HITL stream loop (Cookbook Cell 7)
        print("=== Part A: streaming HITL ===")
        hitl_state = await stream_hitl_until_end_turn(
            client,
            session.id,
            send_events=[
                {
                    "type": "user.message",
                    "content": [{"type": "text", "text": GATE_TASK}],
                }
            ],
            on_custom_tool=make_gate_handler(decisions),
            config=_stream_config(),
            on_event=lambda ev: print_stream_event(ev, preview_length=None),
        )
        print(
            f"custom tool replies: {len(hitl_state.get('responded_ids', set()))}"
        )

        expected = int(os.getenv("OMA_GATE_EXPECT_DECISIONS", "12"))
        if len(decisions) < expected:
            raise RuntimeError(
                f"Expected {expected} receipt decisions, got {len(decisions)}. "
                "The LLM may not have called decide/escalate for every "
                "receipt — retry or adjust GATE_TASK / model."
            )

        lanes = Counter(str(d.get("lane")) for d in decisions.values())
        print(f"\n{len(decisions)} decisions: {dict(lanes)}")

        # Part B — webhooks (Cookbook Cell 9): operate notebook, not run here.
        print(
            "Part B (webhooks / session.status_idled): "
            "see CMA_operate_in_production.ipynb"
        )

        if os.getenv("OMA_KEEP_RESOURCES", "0") != "1":
            await wait_for_idle_status(client, session.id)
            client.sessions.archive(session.id)
            client.environments.archive(env.id)
            client.agents.archive(agent.id)
            print("archived")
        else:
            print(
                f"[KEEP] agent={agent.id}  env={env.id}  session={session.id}"
            )

    finally:
        await client.aclose()


if __name__ == "__main__":
    asyncio.run(main())
