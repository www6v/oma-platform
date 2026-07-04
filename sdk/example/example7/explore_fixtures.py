"""Fixtures for CMA_explore_unfamiliar_codebase (example7)."""

from __future__ import annotations

import io
import textwrap
import zipfile

ENV_NAME = "cookbook-explore-env"
AGENT_NAME = "cookbook-explore"
SESSION_TITLE = "Onboard to repo"

REPO_MOUNT_PATH = "repo.zip"
REPO_UPLOAD_PATH = "/mnt/session/uploads/repo.zip"
DEPLOY_HISTORY_MOUNT = "DEPLOY_HISTORY.md"

EXPLORE_ARCHITECTURE_MARKER = "explore-cookbook-architecture-ok"
EXPLORE_NOTES_MARKER = "explore-cookbook-notes-ok"
EXPLORE_DEPLOY_MARKER = "explore-cookbook-deploy-history-ok"

AGENT_TOOLS = [
    {
        "type": "agent_toolset_20260401",
        "default_config": {
            "enabled": True,
            "permission_policy": {"type": "always_allow"},
        },
    }
]

AGENT_SYSTEM = (
    "You are onboarding to an unfamiliar codebase. Explore before "
    "answering, docs can be stale. Verify what you read against "
    "actual code structure. Write notes to /tmp/NOTES.md as you go."
)

EXPLORE_USER_MESSAGE = (
    f"Unzip {REPO_UPLOAD_PATH} to /tmp/repo/. "
    "Then: what is the actual architecture of this codebase? "
    "Be specific about directory structure. Check if the docs are accurate."
)

NOTES_USER_MESSAGE = "cat /tmp/NOTES.md"

DEPLOY_HISTORY_BYTES = (
    b"# DEPLOY HISTORY\n"
    b"2026-03-01: monolith -> microservices migration complete\n"
)

DEPLOY_FOLLOWUP_MESSAGE = (
    "There's a DEPLOY_HISTORY.md in your workspace now. "
    "Read it and tell me whether it changes anything in "
    "your earlier answer."
)


def make_unfamiliar_repo_zip() -> io.BytesIO:
    """Build in-memory ZIP with stale ARCHITECTURE.md + services/ layout."""
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w") as zf:
        zf.writestr(
            "ARCHITECTURE.md",
            textwrap.dedent("""\
            # Architecture (STALE, do not trust)

            The app is a monolith with three layers:
            - api/ for REST handlers
            - core/ for business logic
            - db/ for database access

            [Out of date. The real structure is microservices under
            services/. An agent that trusts this without reading the
            code will answer wrong.]
        """),
        )
        zf.writestr(
            "README.md",
            "# Widget Service\n\nSee ARCHITECTURE.md (possibly outdated).\n",
        )
        for svc in ["auth", "billing", "notifications", "widgets"]:
            zf.writestr(
                f"services/{svc}/main.py",
                f"# {svc} service entrypoint\ndef handle(): ...\n",
            )
            zf.writestr(
                f"services/{svc}/models.py",
                f"class {svc.title()}: ...\n",
            )
            for i in range(6):
                zf.writestr(
                    f"services/{svc}/util_{i}.py",
                    f"def helper_{i}(): ...\n",
                )
        for legacy in ["api", "core", "db"]:
            zf.writestr(f"{legacy}/.gitkeep", "")
    buf.seek(0)
    return buf


def build_repo_resource(file_id: str) -> dict:
    return {
        "type": "file",
        "file_id": file_id,
        "mount_path": REPO_MOUNT_PATH,
    }
