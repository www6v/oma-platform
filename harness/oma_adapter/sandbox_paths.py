"""AMA-aligned sandbox path normalization for piPy file tools."""

from __future__ import annotations

import os
from pathlib import Path


def root_mount_exists(path: str) -> bool:
    """True when a host-level magic mount exists (symlink or directory)."""
    try:
        os.lstat(path)
        return True
    except OSError:
        return False


def normalize_sandbox_path(workdir: str, path: str) -> str:
    """Rewrite AMA magic paths before piPy resolve_under_cwd.

    Mirrors open-managed-agents LocalSubprocessSandbox.resolvePath().
    """
    normalised = path
    if normalised.startswith("/mnt/session/outputs/") or (
        normalised == "/mnt/session/outputs"
    ):
        if root_mount_exists("/mnt/session/outputs"):
            return normalised
        if normalised == "/mnt/session/outputs":
            return ".mnt/session/outputs"
        return ".mnt/session/outputs/" + normalised[len("/mnt/session/outputs/") :]
    if normalised.startswith("/workspace/"):
        return normalised[len("/workspace/") :]
    if normalised == "/workspace":
        return ""
    if normalised.startswith("/"):
        return normalised[1:]
    return normalised


def resolve_under_sandbox_cwd(cwd: Path, path: str) -> Path:
    """resolve_under_cwd with AMA path rewriting."""
    from pi_coding_agent.tools.path_utils import resolve_under_cwd

    rewritten = normalize_sandbox_path(str(cwd), path)
    return resolve_under_cwd(cwd, rewritten)


def patch_path_utils(workdir: str) -> None:
    """Patch piPy path resolution for one harness turn."""
    import pi_coding_agent.tools.path_utils as path_utils

    del workdir  # normalization uses cwd_path from each tool call

    def resolve(cwd_path: Path, path: str) -> Path:
        return resolve_under_sandbox_cwd(cwd_path, path)

    path_utils.resolve_under_cwd = resolve
