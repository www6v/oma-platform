"""
Skill Examples - High-level helper functions for skill operations.
"""

from __future__ import annotations

import io
import os
import zipfile
from typing import TYPE_CHECKING

import httpx

if TYPE_CHECKING:
    import anthropic

_KEEP = os.getenv("OMA_KEEP_RESOURCES", "0") == "1"


class SkillExamples:
    """Example operations for skills."""

    @staticmethod
    def make_skill_zip(name: str = "sdk-e2e-skill") -> bytes:
        """
        Create a skill zip file for testing.
        
        Args:
            name: Name for the skill
            
        Returns:
            Bytes of the zip file
        """
        buf = io.BytesIO()
        with zipfile.ZipFile(buf, "w", zipfile.ZIP_DEFLATED) as zf:
            zf.writestr(
                "SKILL.md",
                f"---\nname: {name}\ndescription: SDK e2e validation skill. Safe to delete.\n---\n\n# {name}\n",
            )
        return buf.getvalue()

    @staticmethod
    def list_skills(client: anthropic.Anthropic) -> list:
        """
        List all skills.
        
        Args:
            client: Anthropic client instance
            
        Returns:
            List of skills
        """
        page = client.beta.skills.list()
        skills = list(page)
        assert isinstance(skills, list)
        assert len(skills) >= 1  # at least built-in skills exist
        return skills

    @staticmethod
    def retrieve_builtin_skill(client: anthropic.Anthropic, skill_id: str = "builtin_xlsx") -> dict:
        """
        Retrieve a built-in skill by ID.
        
        Args:
            client: Anthropic client instance
            skill_id: ID of the built-in skill to retrieve
            
        Returns:
            Dictionary with skill details
        """
        skill = client.beta.skills.retrieve(skill_id)
        assert skill.id == skill_id
        return {"skill": skill}

    @staticmethod
    def download_version(
        client: anthropic.Anthropic,
        name: str = "sdk-e2e-skill-dl",
    ) -> dict:
        """Upload a skill and download its version as a zip archive."""
        zip_bytes = SkillExamples.make_skill_zip(name)
        resp = client.post(
            "/v1/skills/upload",
            cast_to=httpx.Response,
            files={"file": (f"{name}.zip", zip_bytes, "application/zip")},
            options={"headers": {
                "Content-Type": "multipart/form-data",
                "X-Display-Title": name,
            }},
        )
        assert resp.status_code in (200, 201), resp.text
        skill_id = resp.json()["id"]
        try:
            versions = list(client.beta.skills.versions.list(skill_id))
            assert len(versions) >= 1
            version_id = versions[0].version
            downloaded = client.beta.skills.versions.download(
                version_id, skill_id=skill_id,
            )
            content = downloaded.read()
            assert len(content) > 0
            assert content[:2] == b"PK"
            return {
                "skill_id": skill_id,
                "version": version_id,
                "bytes": len(content),
            }
        finally:
            if not _KEEP:
                client.beta.skills.delete(skill_id)

    @staticmethod
    def upload_and_delete_skill(
        client: anthropic.Anthropic,
        name: str = "sdk-e2e-skill"
    ) -> dict:
        """
        Upload a skill, retrieve it, list its versions, then delete it.
        
        Args:
            client: Anthropic client instance
            name: Name for the skill
            
        Returns:
            Dictionary with skill details
        """
        zip_bytes = SkillExamples.make_skill_zip()
        resp = client.post(
            "/v1/skills/upload",
            cast_to=httpx.Response,
            files={"file": (f"{name}.zip", zip_bytes, "application/zip")},
            options={"headers": {
                "Content-Type": "multipart/form-data",
                "X-Display-Title": name,
            }},
        )
        assert resp.status_code in (200, 201), resp.text
        skill_id = resp.json()["id"]
        try:
            retrieved = client.beta.skills.retrieve(skill_id)
            assert retrieved.id == skill_id

            versions_page = client.beta.skills.versions.list(skill_id)
            assert isinstance(list(versions_page), list)
            return {"skill": retrieved, "versions": list(versions_page), "skill_id": skill_id}
        finally:
            if not _KEEP:
                client.beta.skills.delete(skill_id)
            else:
                print(f"\n[KEEP] skill {skill_id} ({name}) left — delete manually when done")
