"""Tests for sub-agent extension path resolution in oma_adapter."""

from __future__ import annotations

from pathlib import Path

import pytest

from oma_adapter.tools import resolve_subagent_extension_path


def test_resolve_subagent_extension_path_default() -> None:
    path = resolve_subagent_extension_path()
    assert path.name == "subagent_extension.py"
    assert path.is_file()
    assert "piPy-subagent" in str(path)


def test_resolve_subagent_extension_path_env_override(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    custom = tmp_path / "custom_extension.py"
    custom.write_text("# stub\n", encoding="utf-8")
    monkeypatch.setenv("PIPY_SUBAGENT_EXTENSION", str(custom))
    assert resolve_subagent_extension_path() == custom
