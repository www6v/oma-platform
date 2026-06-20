"""E2E tests for /v1/skills — list + upload + retrieve + delete."""

from __future__ import annotations

import io
import os
import zipfile

import anthropic
import httpx

_KEEP = os.getenv("OMA_KEEP_RESOURCES", "0") == "1"


def _make_skill_zip(name: str = "sdk-e2e-skill") -> bytes:
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED) as zf:
        zf.writestr(
            "SKILL.md",
            f"---\nname: {name}\ndescription: SDK e2e validation skill. Safe to delete.\n---\n\n# {name}\n",
        )
    return buf.getvalue()


def test_skills_list(client: anthropic.Anthropic):
    page = client.beta.skills.list()
    skills = list(page)
    assert isinstance(skills, list)
    assert len(skills) >= 1  # at least built-in skills exist


def test_skills_retrieve_builtin(client: anthropic.Anthropic):
    skill = client.beta.skills.retrieve("builtin_xlsx")
    assert skill.id == "builtin_xlsx"


def test_skills_upload_and_delete(client: anthropic.Anthropic):
    zip_bytes = _make_skill_zip()
    resp = client.post(
        "/v1/skills/upload",
        cast_to=httpx.Response,
        files={"file": ("sdk-e2e-skill.zip", zip_bytes, "application/zip")},
        options={"headers": {
            "Content-Type": "multipart/form-data",
            "X-Display-Title": "sdk-e2e-skill",
        }},
    )
    assert resp.status_code in (200, 201), resp.text
    skill_id = resp.json()["id"]
    try:
        retrieved = client.beta.skills.retrieve(skill_id)
        assert retrieved.id == skill_id

        versions_page = client.beta.skills.versions.list(skill_id)
        assert isinstance(list(versions_page), list)
    finally:
        if not _KEEP:
            client.beta.skills.delete(skill_id)
        else:
            print(f"\n[KEEP] skill {skill_id} (sdk-e2e-skill) left — delete manually when done")
