"""Mount resolved skill files into the harness workdir (AMA-aligned)."""

from __future__ import annotations

import base64
import logging
from pathlib import Path
from typing import Any

logger = logging.getLogger(__name__)

SKILL_MOUNT_PREFIX = "home/user/.skills"


def mount_skills(workdir: str, skills: list[dict[str, Any]] | None) -> None:
    """Write skill files under home/user/.skills/<name>/ in the workdir."""
    if not skills:
        return
    root = Path(workdir)
    for skill in skills:
        if skill.get("type") != "skill":
            continue
        name = skill.get("name")
        if not isinstance(name, str) or not name:
            continue
        files = skill.get("files") or []
        if not isinstance(files, list):
            continue
        base = root / SKILL_MOUNT_PREFIX / name
        for item in files:
            if not isinstance(item, dict):
                continue
            filename = item.get("filename")
            if not isinstance(filename, str) or not filename:
                continue
            target = base / filename
            target.parent.mkdir(parents=True, exist_ok=True)
            raw_b64 = item.get("content_base64")
            if isinstance(raw_b64, str) and raw_b64:
                target.write_bytes(base64.b64decode(raw_b64))
                continue
            content = item.get("content")
            if isinstance(content, str):
                target.write_text(content, encoding="utf-8")


def skill_platform_reminders(
    skills: list[dict[str, Any]] | None,
) -> list[dict[str, str]]:
    """Build skill prompt additions for compose_system_prompt."""
    if not skills:
        return []
    out: list[dict[str, str]] = []
    for skill in skills:
        if skill.get("type") != "skill":
            continue
        skill_id = skill.get("skill_id") or skill.get("name") or "skill"
        addition = skill.get("system_prompt_addition")
        if not isinstance(addition, str) or not addition.strip():
            continue
        out.append(
            {
                "source": f"skill:{skill_id}",
                "text": addition.strip(),
            },
        )
    return out
