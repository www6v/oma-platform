"""Skill mount + platform reminder tests."""

from __future__ import annotations

import base64
from pathlib import Path

from oma_adapter.skill_mounter import mount_skills, skill_platform_reminders


def test_mount_skills_writes_skill_md(tmp_path: Path) -> None:
    body = b"# Runbooks\nConsult runbooks first.\n"
    skills = [
        {
            "type": "skill",
            "skill_id": "skill_test",
            "name": "incident-runbooks",
            "system_prompt_addition": "<skill>runbooks</skill>",
            "files": [
                {
                    "filename": "SKILL.md",
                    "content_base64": base64.b64encode(body).decode("ascii"),
                },
            ],
        },
    ]
    mount_skills(str(tmp_path), skills)
    target = tmp_path / "home/user/.skills/incident-runbooks/SKILL.md"
    assert target.read_bytes() == body


def test_skill_platform_reminders_source_tag() -> None:
    reminders = skill_platform_reminders(
        [
            {
                "type": "skill",
                "skill_id": "skill_abc",
                "system_prompt_addition": "Follow the runbook.",
            },
        ],
    )
    assert len(reminders) == 1
    assert reminders[0]["source"] == "skill:skill_abc"
    assert "runbook" in reminders[0]["text"]
