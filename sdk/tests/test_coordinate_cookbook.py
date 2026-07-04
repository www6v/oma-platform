"""Coordinate specialist team cookbook — fixture and helper tests."""

from __future__ import annotations

import os
import sys
from pathlib import Path

import pytest

from oma_sdk import OMAClient
from oma_sdk.subagent import (
    COORDINATE_COMPLETE_MARKER,
    SPECIALIST_LIBRARIAN,
    SPECIALIST_PRICER,
    SPECIALIST_RESEARCHER,
    build_multiagent,
    count_thread_created,
    count_thread_message_received,
)

_EXAMPLE5 = Path(__file__).resolve().parents[1] / "example" / "example5"
if str(_EXAMPLE5) not in sys.path:
    sys.path.insert(0, str(_EXAMPLE5))

from coordinate_fixtures import (  # noqa: E402
    CASE_STUDY_FILES,
    COORDINATOR_SYSTEM,
    MOUNT_CASE_STUDIES,
    MOUNT_PRICING,
    MOUNT_PRODUCT,
    PROPOSAL_OUTPUT_PATH,
    build_prospect_message,
    fixture_path,
    researcher_tools_include_web_search,
)

_RUN_LIVE = os.getenv("OMA_RUN_LIVE_COORDINATE", "0") == "1"


@pytest.fixture
async def oma_client():
    client = OMAClient(base_url=os.getenv("OMA_BASE_URL", "http://localhost:8787"))
    yield client
    await client.aclose()


def test_coordinate_fixture_files_exist() -> None:
    assert fixture_path("northstar/product_one_pager.md").is_file()
    assert fixture_path("northstar/pricing_rules.md").is_file()
    for name in CASE_STUDY_FILES:
        assert fixture_path(f"northstar/case_studies/{name}").is_file()


def test_coordinate_mount_paths() -> None:
    assert MOUNT_PRODUCT.startswith("/mnt/user-data/")
    assert MOUNT_PRICING.startswith("/mnt/user-data/")
    assert MOUNT_CASE_STUDIES.startswith("/mnt/user-data/")


def test_researcher_tools_include_web_search() -> None:
    assert researcher_tools_include_web_search()


def test_coordinator_system_mentions_proposal_path() -> None:
    assert PROPOSAL_OUTPUT_PATH in COORDINATOR_SYSTEM


def test_build_multiagent_three_specialists() -> None:
    roster = build_multiagent(
        ["agt_r", "agt_l", "agt_p"],
        versions={"agt_r": 1, "agt_l": 2, "agt_p": 3},
    )
    assert roster["type"] == "coordinator"
    assert len(roster["agents"]) == 3
    assert roster["agents"][0]["version"] == 1


def test_count_thread_events() -> None:
    events = [
        {"type": "session.thread_created", "session_thread_id": "a"},
        {"type": "session.thread_created", "session_thread_id": "b"},
        {"type": "agent.thread_message_received", "from_agent_name": SPECIALIST_RESEARCHER},
        {"type": "agent.thread_message_received", "from_agent_name": SPECIALIST_LIBRARIAN},
        {"type": "agent.thread_message_received", "from_agent_name": SPECIALIST_PRICER},
        {
            "type": "agent.message",
            "content": [{"type": "text", "text": COORDINATE_COMPLETE_MARKER}],
        },
    ]
    assert count_thread_created(events) == 2
    assert count_thread_message_received(events) == 3


def test_prospect_message_mentions_specialists() -> None:
    blocks = build_prospect_message()
    text = blocks[0]["text"]
    assert "NorthStar" in text
    assert "proposal" in text.lower()
    assert PROPOSAL_OUTPUT_PATH in text


@pytest.mark.live
@pytest.mark.asyncio
@pytest.mark.skipif(
    not _RUN_LIVE,
    reason="set OMA_RUN_LIVE_COORDINATE=1 for live coordinate soak",
)
async def test_coordinate_team_live_soak(oma_client: OMAClient) -> None:
    """CT6: full cookbook soak with real LLM + delegation (opt-in)."""
    from coordinate_team_soak import run_coordinate_team_soak

    model = os.getenv("OMA_MODEL", "qwen3.7-plus")
    result = await run_coordinate_team_soak(
        oma_client,
        model=model,
        keep_resources=os.getenv("OMA_KEEP_RESOURCES") == "1",
    )

    assert result["thread_created_count"] >= 1
    assert result["call_agent_uses"] >= 1
    assert result["has_proposal"] is True

    if os.getenv("OMA_COORDINATE_STRICT") == "1":
        assert result["thread_created_count"] >= 3
        assert result["thread_received_count"] >= 3
        assert result["saw_web_search"] is True

