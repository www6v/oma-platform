#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
Verify with outcome grader — OMA Managed Agents cookbook example.

Mirrors Anthropic ``managed_agents/CMA_verify_with_outcome_grader.ipynb``:
define a rubric, run the agent, let the platform grade output and inject
``<outcome_feedback>`` revision turns until satisfied or max iterations.

Cookbook section → OMA function mapping
---------------------------------------
Setup              → OMAClient, OMA_BASE_URL, OMA_API_KEY
Agent + session    → client.agents.create, client.sessions.create
Define outcome     → user.define_outcome (inline criteria or file rubric)
Drive + stream     → user.message + stream_until_end_turn
Verify             → span.outcome_evaluation_* events + session.outcome_evaluations

Platform coverage (OG1–OG4): in-session outcome supervisor, grade-revise loop,
file rubric via Files API — see CI ``TestOutcomeGrader*``.

Set ``OMA_USE_FILE_RUBRIC=1`` to upload ``rubric.md`` and pass
``rubric: {type: file, file_id}`` (production-style OG4).

Prerequisites
-------------
* Python 3.11+
* ``oma_sdk`` installed (``pip install -e sdk`` from oma-platform)
* OMA server + harness with a real LLM for live runs

Usage::

    OMA_API_KEY=dev-key OMA_BASE_URL=http://127.0.0.1:8787 \\
        python sdk/example/example4/outcome_grader.py
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

from oma_sdk import (
    OMAClient,
    stream_until_end_turn,
    wait_for_idle_status,
)
from oma_sdk.api.agents import MODEL as DEFAULT_MODEL

OMA_BASE_URL = os.getenv("OMA_BASE_URL", "http://127.0.0.1:8787")
os.environ.setdefault("OMA_API_KEY", "dev-key")

ENV_NAME = "cookbook-outcome-env"
AGENT_NAME = "cookbook-outcome-grader"
SESSION_TITLE = "Outcome grader probe"
MODEL = os.getenv("OMA_MODEL", DEFAULT_MODEL["id"])

OUTCOME_DESCRIPTION = (
    "The agent must produce a one-sentence summary that mentions revenue."
)
OUTCOME_CRITERIA = [
    "Mentions revenue explicitly",
    "Exactly one concise sentence",
]
USER_TASK = (
    "Summarize our Q1 revenue trend in one sentence for the exec team."
)

SYSTEM_PROMPT = (
    "You are a concise business analyst. Follow the user's formatting "
    "requests exactly."
)
SCRIPT_DIR = Path(__file__).resolve().parent
RUBRIC_PATH = SCRIPT_DIR / "rubric.md"


def build_define_outcome_event(client_rubric_file_id: str | None) -> dict:
    if client_rubric_file_id:
        return {
            "type": "user.define_outcome",
            "description": OUTCOME_DESCRIPTION,
            "rubric": {
                "type": "file",
                "file_id": client_rubric_file_id,
            },
            "max_iterations": 3,
        }
    return {
        "type": "user.define_outcome",
        "description": OUTCOME_DESCRIPTION,
        "criteria": OUTCOME_CRITERIA,
        "max_iterations": 3,
    }


async def main() -> None:
    use_file_rubric = os.getenv("OMA_USE_FILE_RUBRIC") == "1"
    async with OMAClient(base_url=OMA_BASE_URL) as client:
        env = client.environments.create(
            name=ENV_NAME,
            config={
                "type": "sandbox",
                "sandbox": {
                    "provider": "opensandbox",
                    "opensandbox": {"image": "python:3.12-slim"},
                },
            },
        )
        agent = client.agents.create(
            name=AGENT_NAME,
            model={"id": MODEL},
            system=SYSTEM_PROMPT,
        )
        session = client.sessions.create(
            environment_id=env.id,
            agent={"type": "agent", "id": agent.id, "version": agent.version},
            title=SESSION_TITLE,
        )
        print("Session:", session.id)

        rubric_file_id: str | None = None
        if use_file_rubric:
            rubric_bytes = RUBRIC_PATH.read_bytes()
            uploaded = await client.files.upload(
                filename="outcome-rubric.md",
                content=rubric_bytes,
                media_type="text/markdown",
            )
            rubric_file_id = uploaded["id"]
            print("Rubric file:", rubric_file_id)

        client.sessions.events.send(
            session.id,
            events=[
                build_define_outcome_event(rubric_file_id),
                {
                    "type": "user.message",
                    "content": [{"type": "text", "text": USER_TASK}],
                },
            ],
        )

        await stream_until_end_turn(client, session.id)
        await wait_for_idle_status(client, session.id)

        detail = client.sessions.retrieve(session.id)
        evals = getattr(detail, "outcome_evaluations", None) or []
        if not evals:
            raise SystemExit("expected outcome_evaluations on session")
        last = evals[-1]
        result = last.get("result") if isinstance(last, dict) else getattr(last, "result", None)
        print("Outcome result:", result)
        if result != "satisfied":
            raise SystemExit(f"expected satisfied, got {result!r}")

        if os.getenv("OMA_KEEP_RESOURCES") != "1":
            client.sessions.archive(session.id)
            client.agents.archive(agent.id)
            client.environments.archive(env.id)
            print("Archived session, agent, environment.")


if __name__ == "__main__":
    asyncio.run(main())
