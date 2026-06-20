"""E2E tests for /v1/skills — list + upload + retrieve + delete."""

from __future__ import annotations

import anthropic

from oma_sdk.examples import SkillExamples


def test_skills_list(client: anthropic.Anthropic):
    skills = SkillExamples.list_skills(client)
    assert isinstance(skills, list)
    assert len(skills) >= 1  # at least built-in skills exist


def test_skills_retrieve_builtin(client: anthropic.Anthropic):
    result = SkillExamples.retrieve_builtin_skill(client)
    assert result["skill"].id == "builtin_xlsx"


def test_skills_upload_and_delete(client: anthropic.Anthropic):
    result = SkillExamples.upload_and_delete_skill(client)
    assert result["skill"].id == result["skill_id"]
    assert isinstance(result["versions"], list)
