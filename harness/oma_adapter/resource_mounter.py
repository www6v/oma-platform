"""Best-effort session resource mounting before a harness turn."""

from __future__ import annotations

import base64
import logging
import os
from pathlib import Path
from typing import Any

logger = logging.getLogger(__name__)


def ensure_session_output_mounts(workdir: str) -> None:
    """Ensure workdir-relative output paths symlink to the canonical mount."""
    root = Path(workdir)
    canonical = root / ".mnt" / "session" / "outputs"
    aliases = (
        root / "mnt" / "session" / "outputs",
        root / "outputs",
    )
    target = _resolve_output_target(canonical, *aliases)
    if target is None:
        canonical.parent.mkdir(parents=True, exist_ok=True)
        canonical.mkdir(parents=True, exist_ok=True)
        target = canonical.resolve()
    for alias in aliases:
        _ensure_output_symlink(alias, target)


def _resolve_output_target(*paths: Path) -> Path | None:
    for path in paths:
        if path.is_symlink():
            return path.resolve()
        if path.is_dir():
            return path.resolve()
    return None


def _ensure_output_symlink(link: Path, target: Path) -> None:
    if link.is_symlink():
        try:
            if link.resolve() == target.resolve():
                return
        except OSError:
            pass
        link.unlink()
    elif link.exists():
        return
    link.parent.mkdir(parents=True, exist_ok=True)
    link.symlink_to(target, target_is_directory=True)


def mount_resources(
    workdir: str,
    resources: list[dict[str, Any]] | None,
) -> dict[str, str]:
    """Mount resources into workdir; returns env vars applied for the turn."""
    if not resources:
        return {}
    root = Path(workdir)
    env_batch: dict[str, str] = {}
    for res in resources:
        try:
            res_type = res.get("type")
            if res_type == "file":
                _mount_file(root, res)
            elif res_type == "memory_store":
                _mount_memory_store(root, res)
            elif res_type in ("env", "env_secret"):
                name = res.get("name")
                value = res.get("value")
                if isinstance(name, str) and isinstance(value, str) and name:
                    env_batch[name] = value
            elif res_type in ("github_repository", "github_repo"):
                logger.warning(
                    "github_repository mount skipped in local harness (url=%s)",
                    res.get("url") or res.get("repo_url"),
                )
            else:
                logger.warning("unknown resource type %s; skipping", res_type)
        except Exception:
            logger.exception(
                "resource mount failed type=%s id=%s; skipping",
                res.get("type"),
                res.get("id"),
            )
    if env_batch:
        os.environ.update(env_batch)
    return env_batch


def _normalize_file_mount_path(mount_path: str, file_id: str) -> str:
    """Map cookbook bare filenames to AMA upload paths."""
    cleaned = mount_path.strip()
    if not cleaned:
        return f"/mnt/session/uploads/{file_id}"
    if cleaned.startswith("/mnt/"):
        return cleaned
    basename = cleaned.lstrip("/")
    if "/" not in basename:
        return f"/mnt/session/uploads/{basename}"
    return cleaned if cleaned.startswith("/") else f"/{cleaned}"


def _mount_file(root: Path, res: dict[str, Any]) -> None:
    raw_mount = res.get("mount_path")
    file_id = str(res.get("file_id") or "file")
    if isinstance(raw_mount, str):
        mount_path = _normalize_file_mount_path(raw_mount, file_id)
    else:
        mount_path = f"/mnt/session/uploads/{file_id}"
    rel = _relative_mount_path(mount_path)
    target = root / rel
    target.parent.mkdir(parents=True, exist_ok=True)
    raw_b64 = res.get("content_base64")
    if isinstance(raw_b64, str) and raw_b64:
        data = base64.b64decode(raw_b64)
        target.write_bytes(data)
        return
    content = res.get("content")
    if isinstance(content, str):
        target.write_text(content, encoding="utf-8")


def _mount_memory_store(root: Path, res: dict[str, Any]) -> None:
    store_name = res.get("store_name") or res.get("store_id") or "memory"
    read_only = bool(res.get("read_only"))
    base = root / "mnt" / "memory" / str(store_name)
    base.mkdir(parents=True, exist_ok=True)
    memories = res.get("memories") or []
    if not isinstance(memories, list):
        return
    for item in memories:
        if not isinstance(item, dict):
            continue
        path = item.get("path")
        content = item.get("content")
        if not isinstance(path, str) or not path:
            continue
        if not isinstance(content, str):
            content = ""
        target = base / path.lstrip("/")
        target.parent.mkdir(parents=True, exist_ok=True)
        if read_only and target.exists():
            continue
        target.write_text(content, encoding="utf-8")


def _relative_mount_path(mount_path: str) -> Path:
    cleaned = mount_path.strip()
    if cleaned.startswith("/"):
        cleaned = cleaned.lstrip("/")
    return Path(cleaned)
