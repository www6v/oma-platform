"""Outcome grader cookbook tests — fixtures (CI) + live EV soak (opt-in)."""

from __future__ import annotations

import os
import sys
from pathlib import Path

import pytest

from oma_sdk import OMAClient

_EXAMPLE4 = Path(__file__).resolve().parents[1] / "example" / "example4"
if str(_EXAMPLE4) not in sys.path:
    sys.path.insert(0, str(_EXAMPLE4))

from ev_charging_fixtures import (  # noqa: E402
    build_kickoff_message,
    load_rubric,
    load_system_prompt,
    load_task,
    rubric_has_cookbook_checklist,
)

_RUN_LIVE_EV = os.getenv("OMA_RUN_LIVE_OUTCOME_EV", "0") == "1"
BRIEF_PATH_MARKER = "/mnt/session/outputs/brief.md"


@pytest.fixture
async def oma_client():
    client = OMAClient(base_url=os.getenv("OMA_BASE_URL", "http://localhost:8787"))
    yield client
    await client.aclose()


def test_ev_charging_task_fixture() -> None:
    task = load_task()
    assert "DC fast charging" in task
    assert "Capex range" in task
    assert "Hardware vs install cost split" in task


def test_ev_charging_rubric_fixture() -> None:
    rubric = load_rubric()
    assert len(rubric) > 2000
    assert rubric_has_cookbook_checklist(rubric)
    assert "web_fetch" in rubric
    assert BRIEF_PATH_MARKER in rubric


def test_ev_charging_system_prompt_fixture() -> None:
    prompt = load_system_prompt()
    assert "brief.md" in prompt
    assert "Sources section" in prompt


def test_ev_charging_kickoff_includes_task() -> None:
    task = load_task()
    kickoff = build_kickoff_message(task)
    assert "DC fast charging" in kickoff
    assert BRIEF_PATH_MARKER in kickoff


@pytest.mark.live
@pytest.mark.asyncio
@pytest.mark.skipif(
    not _RUN_LIVE_EV,
    reason="set OMA_RUN_LIVE_OUTCOME_EV=1 for live EV charging soak",
)
async def test_outcome_ev_charging_live_soak(oma_client: OMAClient) -> None:
    """OG5: full cookbook soak with real LLM + web tools (opt-in)."""
    from ev_charging_soak import run_ev_charging_outcome_soak

    model = os.getenv("OMA_MODEL", "qwen3.7-plus")
    result = await run_ev_charging_outcome_soak(
        oma_client,
        model=model,
        use_file_rubric=True,
        keep_resources=os.getenv("OMA_KEEP_RESOURCES") == "1",
    )

    assert result["outcome_pass_count"] >= 1
    assert result["terminal_result"] in {
        "satisfied",
        "max_iterations_reached",
    }
    assert result["has_brief"] is True

    if os.getenv("OMA_EV_STRICT") == "1":
        assert result["terminal_result"] == "satisfied"
