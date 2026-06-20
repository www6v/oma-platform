"""E2E tests for /v1/skills — list + upload + retrieve + delete."""

from __future__ import annotations

import io
import zipfile

from oma_sdk import OMAClient


def _make_skill_zip(name: str = "sdk-e2e-skill") -> bytes:
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED) as zf:
        zf.writestr(
            "SKILL.md",
            f"---\nname: {name}\ndescription: SDK e2e validation skill. Safe to delete.\n---\n\n# {name}\n",
        )
    return buf.getvalue()


async def test_skills_list(client: OMAClient):
    page = client.skills.list()
    skills = list(page)
    assert isinstance(skills, list)
    assert len(skills) >= 1  # at least built-in skills exist


async def test_skills_retrieve_builtin(client: OMAClient):
    # builtin_xlsx is always present
    skill = client.skills.retrieve("builtin_xlsx")
    assert skill.id == "builtin_xlsx"


async def test_skills_upload_and_delete(client: OMAClient):
    zip_bytes = _make_skill_zip()
    r = await client._http.post(
        "/v1/skills/upload",
        files={"file": ("sdk-e2e-skill.zip", zip_bytes, "application/zip")},
        headers={"X-Display-Title": "sdk-e2e-skill"},
    )
    assert r.status_code in (200, 201), r.text
    skill_id = r.json()["id"]
    try:
        retrieved = client.skills.retrieve(skill_id)
        assert retrieved.id == skill_id

        versions_page = client.skills.versions.list(skill_id)
        assert isinstance(list(versions_page), list)
    finally:
        client.skills.delete(skill_id)
