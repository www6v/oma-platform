"""AMA-aligned sandbox path normalization for piPy file tools."""

from __future__ import annotations

import os
from pathlib import Path

_REMOTE_SANDBOX_PROVIDERS = frozenset(
    {"e2b", "daytona", "litebox", "boxlite", "boxrun"},
)


def is_remote_sandbox_provider(provider: str | None) -> bool:
    """True when bash should route through platform exec API."""
    return (provider or "local").lower() in _REMOTE_SANDBOX_PROVIDERS


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
    del workdir  # reserved for future workdir-specific rules
    normalised = path
    if normalised.startswith("/mnt/session/outputs/") or (
        normalised == "/mnt/session/outputs"
    ):
        if root_mount_exists("/mnt/session/outputs"):
            return normalised
        if normalised == "/mnt/session/outputs":
            return ".mnt/session/outputs"
        return ".mnt/session/outputs/" + normalised[len("/mnt/session/outputs/") :]
    if normalised.startswith("/mnt/session/uploads/") or (
        normalised == "/mnt/session/uploads"
    ):
        if root_mount_exists("/mnt/session/uploads"):
            return normalised
        if normalised == "/mnt/session/uploads":
            return "mnt/session/uploads"
        return "mnt/session/uploads/" + normalised[len("/mnt/session/uploads/") :]
    if normalised.startswith("/mnt/user-data/") or normalised == "/mnt/user-data":
        if root_mount_exists("/mnt/user-data"):
            return normalised
        if normalised == "/mnt/user-data":
            return "mnt/user-data"
        return "mnt/user-data/" + normalised[len("/mnt/user-data/") :]
    if normalised.startswith("/mnt/memory/") or normalised == "/mnt/memory":
        if root_mount_exists("/mnt/memory"):
            return normalised
        if normalised == "/mnt/memory":
            return ".mnt/memory"
        return ".mnt/memory/" + normalised[len("/mnt/memory/") :]
    if normalised.startswith("/workspace/"):
        return normalised[len("/workspace/") :]
    if normalised == "/workspace":
        return ""
    if normalised.startswith("/"):
        return normalised[1:]
    return normalised


def _resolve_under_cwd(cwd: Path, path: str) -> Path:
    """Mirror pi_coding_agent.tools.path_utils.resolve_under_cwd."""
    candidate = Path(path)
    if not candidate.is_absolute():
        candidate = cwd / candidate
    resolved = candidate.resolve()
    cwd_resolved = cwd.resolve()
    if resolved != cwd_resolved and cwd_resolved not in resolved.parents:
        raise ValueError(f"Path escapes working directory: {path}")
    return resolved


def resolve_under_sandbox_cwd(cwd: Path, path: str) -> Path:
    """resolve_under_cwd with AMA path rewriting."""
    rewritten = normalize_sandbox_path(str(cwd), path)
    return _resolve_under_cwd(cwd, rewritten)


def patch_path_utils(workdir: str) -> None:
    """Patch piPy path resolution for one harness turn."""
    import pi_coding_agent.tools.path_utils as path_utils

    del workdir  # normalization uses cwd_path from each tool call

    def resolve(cwd_path: Path, path: str) -> Path:
        return resolve_under_sandbox_cwd(cwd_path, path)

    path_utils.resolve_under_cwd = resolve


def rewrite_bash_session_paths(command: str, cwd: str) -> str:
    """Rewrite AMA magic paths in bash commands to workdir-local mounts.

    piPy file tools use normalize_sandbox_path; bash subprocesses do not.
    When host-level /mnt/session/* mounts are absent, redirect to the session
    workdir paths created by the platform (outputs symlink + upload mounts).
    """
    markers = (
        "/mnt/session/outputs",
        "/mnt/session/uploads",
        "/mnt/user-data",
        "/mnt/memory",
    )
    for marker in markers:
        if marker not in command:
            continue
        if root_mount_exists(marker):
            continue
        rel = normalize_sandbox_path(cwd, marker)
        local = str((Path(cwd) / rel).resolve())
        command = command.replace(marker, local)
    return command


# Backward-compatible alias used by outbound bash ops.
rewrite_bash_session_output_paths = rewrite_bash_session_paths
