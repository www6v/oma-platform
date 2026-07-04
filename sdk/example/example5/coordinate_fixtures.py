"""Fixtures for CMA_coordinate_specialist_team (example5)."""

from __future__ import annotations

from pathlib import Path
from typing import Any

SCRIPT_DIR = Path(__file__).resolve().parent

MOUNT_PRODUCT = "/mnt/user-data/product_one_pager.md"
MOUNT_PRICING = "/mnt/user-data/pricing_rules.md"
MOUNT_CASE_STUDIES = "/mnt/user-data/case_studies"

PROPOSAL_FILENAME = "proposal.md"
PROPOSAL_OUTPUT_PATH = f"/mnt/session/outputs/{PROPOSAL_FILENAME}"

ENV_NAME = "cookbook-coordinate-env"
COORDINATOR_NAME = "proposal_writer"
SESSION_TITLE = "NorthStar proposal coordination"

CASE_STUDY_FILES = (
    "regional_health.md",
    "metro_clinic.md",
)

PROSPECT_NAME = "NorthStar Regional Health System"
PROSPECT_BEDS = 2000


def _toolset(*names: str) -> list[dict[str, Any]]:
    return [
        {
            "type": "agent_toolset_20260401",
            "default_config": {
                "enabled": True,
                "permission_policy": {"type": "always_allow"},
            },
            "configs": [{"name": name} for name in names],
        }
    ]


RESEARCHER_TOOLS = [
    {
        "type": "agent_toolset_20260401",
        "default_config": {
            "enabled": False,
            "permission_policy": {"type": "always_allow"},
        },
        "configs": [
            {"name": "web_search", "enabled": True},
            {"name": "read", "enabled": True},
            {"name": "write", "enabled": True},
            {"name": "bash", "enabled": False},
            {"name": "web_fetch", "enabled": False},
        ],
    }
]
LIBRARIAN_TOOLS = _toolset("read", "grep", "glob", "write")
PRICER_TOOLS = _toolset("read", "write")
COORDINATOR_TOOLS = [{"type": "agent_toolset_20260401"}]

RESEARCHER_SYSTEM = (
    "You are prospect_researcher. You MUST call the web_search tool "
    "(not web_fetch or bash) to research the target health system. "
    "Return compact JSON with keys: priorities, recent_moves, "
    "pain_points, sources."
)

LIBRARIAN_SYSTEM = (
    "You are case_study_picker. Use the read tool (not bash) on the "
    "mounted case study files:\n"
    f"- {MOUNT_CASE_STUDIES}/regional_health.md\n"
    f"- {MOUNT_CASE_STUDIES}/metro_clinic.md\n"
    "Return JSON with key picks: list of {file, customer, why_relevant}."
)

PRICER_SYSTEM = (
    "You are pricing_modeler. Use the read tool (not bash) on "
    f"{MOUNT_PRICING} and {MOUNT_PRODUCT}. "
    "Return JSON with key options: two pricing tiers scaled for the prospect."
)

COORDINATOR_SYSTEM = (
    "You are proposal_writer, a sales coordinator. Delegate in parallel to "
    "prospect_researcher (research), case_study_picker (case studies), and "
    "pricing_modeler (pricing). Synthesize their JSON reports, then call "
    "write with path="
    f'"{PROPOSAL_OUTPUT_PATH}" and content=the full proposal markdown. '
    "Use call_agent tools for each specialist."
)


def fixture_path(rel: str) -> Path:
    return SCRIPT_DIR / rel


def case_study_mount_path(filename: str) -> str:
    return f"{MOUNT_CASE_STUDIES}/{filename}"


def build_prospect_message() -> list[dict[str, str]]:
    text = (
        f"Draft a sales proposal for NorthStar Health targeting "
        f"{PROSPECT_NAME} (~{PROSPECT_BEDS} beds). "
        "Delegate prospect research, case study selection, and pricing to "
        "your specialists. Write the final proposal with the write tool using "
        f'path="{PROPOSAL_OUTPUT_PATH}" and content=full markdown.'
    )
    return [{"type": "text", "text": text}]


def researcher_tools_include_web_search() -> bool:
    configs = RESEARCHER_TOOLS[0].get("configs") or []
    names = {entry.get("name") for entry in configs if isinstance(entry, dict)}
    return "web_search" in names
