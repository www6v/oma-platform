"""Fixtures for CMA_orchestrate_issue_to_pr (example8)."""

from __future__ import annotations

import io
import zipfile
from pathlib import Path

FIXTURE_DIR = Path(__file__).resolve().parent / "orchestrate"

ENV_NAME = "cookbook-orchestrate-env"
AGENT_NAME = "cookbook-orchestrate"
SESSION_TITLE = "Issue #42 → PR"

REPO_MOUNT_PATH = "repo.zip"
REPO_UPLOAD_PATH = "/mnt/session/uploads/repo.zip"
USER_WORKDIR = "/mnt/user"
PR_STATE_PATH = f"{USER_WORKDIR}/.gh-state/pr_101.json"

ORCHESTRATE_TURN1_MARKER = "orchestrate-cookbook-turn-1-ok"
ORCHESTRATE_VERIFY_MARKER = "orchestrate-cookbook-verify-ok"

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
    "You are a maintainer bot. You read issues via `./gh-mock`, explore "
    "the codebase, write fixes, and shepherd PRs through CI and review. "
    "When CI fails or a reviewer requests changes, read what they said "
    "and address it, don't just retry blindly.\n\n"
    f"Work in {USER_WORKDIR}. All gh-mock commands run from there."
)

ENV_CONFIG = {
    "type": "cloud",
    "networking": {"type": "limited", "allow_package_managers": True},
    "packages": {"pip": ["pytest"]},
}

CHAIN_USER_MESSAGE = (
    f"Unpack {REPO_UPLOAD_PATH} into {USER_WORKDIR} "
    "and ship a fix for issue #42 end-to-end. Read the "
    "./gh-mock script first to see what subcommands it "
    "supports; use those to view the issue, open a PR, "
    "run CI, handle review feedback, and merge. Show me "
    "the final PR state when you're done."
)

VERIFY_USER_MESSAGE = (
    f"Print the contents of {PR_STATE_PATH} so "
    "I can see the final state, CI status, and reviews."
)


def make_orchestrate_repo_zip() -> io.BytesIO:
    """Pack example8/orchestrate/ into an in-memory zip (mirrors cookbook)."""
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w") as zf:
        for path in FIXTURE_DIR.rglob("*"):
            if not path.is_file() or path.name == "README.md":
                continue
            zf.write(path, path.relative_to(FIXTURE_DIR).as_posix())
    buf.seek(0)
    return buf


def build_repo_resource(file_id: str) -> dict:
    return {
        "type": "file",
        "file_id": file_id,
        "mount_path": REPO_MOUNT_PATH,
    }
