"""Tests for resource_mounter."""

from __future__ import annotations

import base64
import os
from pathlib import Path

from oma_adapter.resource_mounter import mount_resources


def test_mount_file_env_and_memory_store(tmp_path: Path) -> None:
    workdir = tmp_path / "wd"
    workdir.mkdir()
    env = mount_resources(
        str(workdir),
        [
            {
                "type": "file",
                "mount_path": "/mnt/session/uploads/demo.txt",
                "content_base64": base64.b64encode(b"demo").decode("ascii"),
            },
            {
                "type": "memory_store",
                "store_name": "team",
                "memories": [
                    {"path": "notes/todo.md", "content": "- ship T13"},
                ],
            },
            {"type": "env", "name": "MOUNT_TEST", "value": "yes"},
        ],
    )
    assert (workdir / "mnt/session/uploads/demo.txt").read_text() == "demo"
    assert (
        workdir / "mnt/memory/team/notes/todo.md"
    ).read_text() == "- ship T13"
    assert env["MOUNT_TEST"] == "yes"
    assert os.environ.get("MOUNT_TEST") == "yes"
    os.environ.pop("MOUNT_TEST", None)
