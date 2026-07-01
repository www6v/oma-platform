#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Iterate: do → observe → fix — OMA Managed Agents parity probe.

Mirrors Anthropic cookbook ``managed_agents/CMA_iterate_fix_failing_tests.ipynb``:
upload a tiny package with planted bugs, drive the agent to make tests pass,
verify independently, then archive.

Cookbook section → OMA function mapping
---------------------------------------
Cell 1   Setup              → OMAClient, OMA_BASE_URL, OMA_API_KEY
§1 Cell 3  Agent            → client.agents.create
§2 Cell 5  Environment      → client.environments.create (limited networking)
§3 Cell 7  Upload fixtures  → upload_fixture() → client.files.upload
§4 Cell 9  Session + mount  → client.sessions.create(resources=[...])
§5 Cell 11 Drive + stream   → stream_until_end_turn() (open stream, then send)
§6 Cell 15 Verify           → second user.message + stream_until_end_turn
§7 Cell 17 Clean up         → wait_for_idle_status + archive session/env/agent

This script is a **parity probe**: failures usually indicate a platform gap,
not a missing workaround. See ``sdk/SDK-PLAN.md`` § Cookbook parity — iterate.

Prerequisites
-------------
* Python 3.11+
* ``oma_sdk`` installed (``pip install -e sdk`` from oma-platform)
* OMA server at ``OMA_BASE_URL`` with ``OMA_API_KEY`` and harness running

Usage::

    OMA_API_KEY=dev-key OMA_BASE_URL=http://127.0.0.1:8787 \\
        python sdk/example/example2/iterate_fix_failing_tests.py

``python`` and ``python3`` both work; if ``python`` is Python 2.x the launcher
re-execs with ``python3`` automatically.

Set ``OMA_KEEP_RESOURCES=1`` to skip archive at the end.
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
import os
from pathlib import Path

from oma_sdk import (
    OMAClient,
    StreamConfig,
    print_stream_event,
    stream_until_end_turn,
    wait_for_idle_status,
)
from oma_sdk.api.agents import MODEL as DEFAULT_MODEL

# ---------------------------------------------------------------------------
# Cookbook mapping: notebook Cell 1 (setup)
# ---------------------------------------------------------------------------
# Cookbook:
#   from anthropic import Anthropic
#   client = Anthropic()
#   FIXTURE = Path("example_data") / "iterate"
# OMA equivalent:
#   OMAClient(base_url=...) routes managed-agents REST to your OMA server.
OMA_BASE_URL = os.getenv("OMA_BASE_URL", "http://127.0.0.1:8787")
os.environ.setdefault("OMA_API_KEY", "dev-key")

ENV_NAME = "cookbook-iterate-env"
AGENT_NAME = "cookbook-iterate"
SESSION_TITLE = "Get the tests green"
MODEL = os.getenv("OMA_MODEL", DEFAULT_MODEL["id"])

SCRIPT_DIR = Path(__file__).resolve().parent
# Cookbook: FIXTURE = Path("example_data") / "iterate"
FIXTURE_DIR = SCRIPT_DIR / "iterate"
CALC_PATH = FIXTURE_DIR / "calc.py"
TEST_PATH = FIXTURE_DIR / "test_calc.py"
OUTPUT_PATH = SCRIPT_DIR / "calc_fixed.py"

# Cookbook Cell 9: mount_path is the filename only; files appear under
# /mnt/session/uploads/<mount_path> (read-only).
CALC_MOUNT = "calc.py"
TEST_MOUNT = "test_calc.py"

# ---------------------------------------------------------------------------
# Cookbook mapping: notebook Cell 3 (§1 Create the agent)
# ---------------------------------------------------------------------------
# System prompt is deliberately sparse — the test output makes the task obvious.
ITERATE_SYSTEM_PROMPT = (
    "You are a debugging agent. Your job is to make failing tests pass. "
    "Run the tests, read the failures, fix the code, repeat until green. "
    "Stop when every assertion passes."
)

# Cookbook Cell 3: agent_toolset_20260401 with always_allow permission policy.
TOOLS = [
    {
        "type": "agent_toolset_20260401",
        "default_config": {
            "enabled": True,
            "permission_policy": {"type": "always_allow"},
        },
    }
]

# Cookbook Cell 11: initial task — copy to /mnt/user, iterate, write output.
ITERATE_TASK = (
    "The tests in /mnt/session/uploads/test_calc.py are failing. "
    "Copy both files into /mnt/user, iterate on calc.py until every test "
    "passes, then write the final calc.py to /mnt/session/outputs/calc.py. "
    "pytest isn't installed here, run the assertions directly with "
    "`python3 -c ...` instead."
)

# Cookbook Cell 15: independent verification after the agent claims success.
VERIFY_TASK = (
    "Re-run every assertion from /mnt/session/uploads/test_calc.py one more "
    "time against your final calc.py with `python3 -c ...` to confirm they "
    "all pass, then cat the final /mnt/session/outputs/calc.py."
)


def _load_dotenv() -> None:
    """Load oma-platform/.env when present (cookbook uses python-dotenv)."""
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


async def upload_fixture(
    client: OMAClient,
    path: Path,
    mime: str,
) -> dict:
    """Cookbook Cell 7: client.beta.files.upload(file=(name, bytes, mime)).

    Gap F1: OMA uses ``client.files.upload`` (httpx async), not
    ``client.beta.files``. Same wire route, different SDK surface (T20).
    """
    payload = path.read_bytes()
    return await client.files.upload(
        file=(path.name, io.BytesIO(payload), mime),
        downloadable=True,
    )


async def download_output_calc(
    client: OMAClient,
    session_id: str,
    dest: Path,
) -> None:
    """Download agent-written calc.py from session outputs via Files API.

    Cookbook Cell 15 asks the agent to cat ``/mnt/session/outputs/calc.py``.
    Parity probe: ``files.list(scope_id=session.id)`` must include calc.py
    after turn sync (same gap class as data-analyst report.html — O1).
    """
    outputs = await client.files.list(scope_id=session_id)
    files = outputs.get("data") or []
    calc_file = next(
        (
            f for f in files
            if f.get("filename") == "calc.py"
            and str(f.get("id", "")).startswith("out:")
        ),
        None,
    )
    if calc_file is None:
        names = [f.get("filename") for f in files]
        raise RuntimeError(
            "calc.py not found via Files API "
            f"(scope_id={session_id}, files={names}). "
            "Platform parity gap O1 — outputs sync may be missing."
        )
    content = await client.files.download(calc_file["id"])
    dest.write_bytes(content)
    print(f"Downloaded {dest.name} ({len(content)} bytes) via Files API")


async def main() -> None:
    _load_dotenv()

    print(f"OMA base URL : {OMA_BASE_URL}")
    print(f"Fixtures     : {FIXTURE_DIR}  (exists: {FIXTURE_DIR.is_dir()})")
    if not CALC_PATH.is_file() or not TEST_PATH.is_file():
        raise FileNotFoundError(
            f"Missing fixture files under {FIXTURE_DIR}"
        )

    client = OMAClient(base_url=OMA_BASE_URL)

    try:
        # ------------------------------------------------------------------
        # §1 Create the agent — Cookbook Cell 3
        # ------------------------------------------------------------------
        # Cookbook: client.beta.agents.create(model=MODEL, ...)
        # OMA: model is {"id": ...} dict — gap M1 if server rejects bare string.
        agent = client.agents.create(
            name=AGENT_NAME,
            model={"id": MODEL},
            system=ITERATE_SYSTEM_PROMPT,
            tools=TOOLS,
        )
        print(f"Created agent: id={agent.id}  version={agent.version}")

        # ------------------------------------------------------------------
        # §2 Create the environment — Cookbook Cell 5
        # ------------------------------------------------------------------
        # Cookbook: cloud + limited networking (no outbound — no pip needed).
        # Iterate uses ``python3 -c`` instead of pytest; no packages block.
        env = client.environments.create(
            name=ENV_NAME,
            config={"type": "cloud", "networking": {"type": "limited"}},
        )
        print(f"Created environment: {env.id}")

        # ------------------------------------------------------------------
        # §3 Upload the failing tests — Cookbook Cell 7
        # ------------------------------------------------------------------
        calc_file = await upload_fixture(client, CALC_PATH, "text/x-python")
        test_file = await upload_fixture(client, TEST_PATH, "text/x-python")
        print(f"uploaded: {calc_file['id']}, {test_file['id']}")

        # ------------------------------------------------------------------
        # §4 Create the session — Cookbook Cell 9
        # ------------------------------------------------------------------
        # resources[] mounts files at /mnt/session/uploads/<mount_path>.
        session = client.sessions.create(
            environment_id=env.id,
            agent={"type": "agent", "id": agent.id, "version": agent.version},
            resources=[
                {
                    "type": "file",
                    "file_id": calc_file["id"],
                    "mount_path": CALC_MOUNT,
                },
                {
                    "type": "file",
                    "file_id": test_file["id"],
                    "mount_path": TEST_MOUNT,
                },
            ],
            title=SESSION_TITLE,
        )
        print(f"session: {session.id}")

        resources = getattr(session, "resources", None) or []
        if len(resources) < 2:
            raise RuntimeError(
                "session.resources missing mounted files — platform gap S1"
            )
        print(f"Session resources: {resources}")

        # ------------------------------------------------------------------
        # §5 Drive the agent and watch it work — Cookbook Cell 11
        # ------------------------------------------------------------------
        # Canonical pattern: open stream, send user.message inside, exit on
        # end_turn. stream_until_end_turn wraps the OMA httpx SSE equivalent.
        print("--- iterate loop ---")
        await stream_until_end_turn(
            client,
            session.id,
            send_events=[
                {
                    "type": "user.message",
                    "content": [{"type": "text", "text": ITERATE_TASK}],
                }
            ],
            config=_stream_config(),
            on_event=lambda ev: print_stream_event(ev, preview_length=None),
        )

        # ------------------------------------------------------------------
        # §6 Verify — Cookbook Cell 15
        # ------------------------------------------------------------------
        # Second turn in the same session: don't trust the agent's word.
        # MT1: covered by Go TestIterateCookbookMultiTurn + pytest test_iterate_cookbook.py.
        await stream_until_end_turn(
            client,
            session.id,
            send_events=[
                {
                    "type": "user.message",
                    "content": [{"type": "text", "text": VERIFY_TASK}],
                }
            ],
            config=_stream_config(),
            on_event=lambda ev: print_stream_event(ev, preview_length=None),
        )

        await download_output_calc(client, session.id, OUTPUT_PATH)

        # ------------------------------------------------------------------
        # §7 Cleanup — Cookbook Cell 17
        # ------------------------------------------------------------------
        # Cookbook archives session, environment, and agent (all three).
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
