"""Cookbook fixtures for OG5 — EV fast-charging outcome grader live soak."""

from __future__ import annotations

from pathlib import Path

FIXTURE_DIR = Path(__file__).resolve().parent / "ev_charging"
TASK_PATH = FIXTURE_DIR / "task.txt"
RUBRIC_PATH = FIXTURE_DIR / "rubric.md"
SYSTEM_PROMPT_PATH = FIXTURE_DIR / "system_prompt.txt"

BRIEF_FILENAME = "brief.md"
BRIEF_OUTPUT_PATH = f"/mnt/session/outputs/{BRIEF_FILENAME}"
MAX_ITERATIONS = 5

ENV_NAME = "cookbook-outcome-ev-env"
AGENT_NAME = "cookbook-outcome-ev-writer"
SESSION_TITLE = "Brief: EV fast-charging unit economics"

WRITER_TOOLS = [
    {
        "type": "agent_toolset_20260401",
        "default_config": {
            "enabled": True,
            "permission_policy": {"type": "always_allow"},
        },
        "configs": [
            {"name": "web_search"},
            {"name": "web_fetch"},
            {"name": "read"},
            {"name": "write"},
        ],
    }
]

def load_task() -> str:
    return TASK_PATH.read_text(encoding="utf-8").strip()


def load_rubric() -> str:
    return RUBRIC_PATH.read_text(encoding="utf-8").strip()


def load_system_prompt() -> str:
    return SYSTEM_PROMPT_PATH.read_text(encoding="utf-8").strip()


def build_kickoff_message(task: str) -> str:
    """User turn text — must include the task; define_outcome is for grading only."""
    return (
        f"{task.strip()}\n\n"
        f"Save the finished brief to {BRIEF_OUTPUT_PATH}."
    )


def rubric_has_cookbook_checklist(rubric: str) -> bool:
    """True when rubric contains the seven-item EV charging checklist."""
    needles = (
        "Capex range",
        "Demand charges",
        "Utilization breakeven",
        "NEVI",
        "sec.gov",
        "Contrarian source",
        "Cost split",
    )
    return all(item in rubric for item in needles)
