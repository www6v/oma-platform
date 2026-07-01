"""Tests for environment package installation (E1)."""

from __future__ import annotations

from unittest.mock import patch

import pytest

from oma_adapter.env_packages import (
    ensure_environment_packages,
    harness_venv_bin,
    pip_packages_from_environment,
)


def test_pip_packages_from_environment_config_shape() -> None:
    env = {
        "id": "env_1",
        "config": {
            "type": "cloud",
            "packages": {"pip": ["pandas", "plotly"]},
        },
    }
    assert pip_packages_from_environment(env) == ["pandas", "plotly"]


def test_pip_packages_from_top_level_packages() -> None:
    env = {"packages": {"pip": ["requests"]}}
    assert pip_packages_from_environment(env) == ["requests"]


def test_pip_packages_empty_when_missing() -> None:
    assert pip_packages_from_environment(None) == []
    assert pip_packages_from_environment({}) == []
    assert pip_packages_from_environment({"config": {}}) == []


def test_harness_venv_bin_is_directory() -> None:
    path = harness_venv_bin()
    assert path
    assert __import__("os").path.isdir(path)


def test_ensure_skipped_when_env_flag_set(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("OMA_SKIP_ENV_PIP", "1")
    env = {"config": {"packages": {"pip": ["pandas"]}}}
    assert ensure_environment_packages(env) == []


def test_ensure_marks_already_importable_without_pip(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.delenv("OMA_SKIP_ENV_PIP", raising=False)
    env = {"config": {"packages": {"pip": ["httpx"]}}}
    with patch("oma_adapter.env_packages._run_pip_install") as mock_install:
        result = ensure_environment_packages(env)
    assert result == ["httpx"]
    mock_install.assert_not_called()


def test_ensure_calls_pip_for_missing_packages(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.delenv("OMA_SKIP_ENV_PIP", raising=False)
    env = {"config": {"packages": {"pip": ["oma-nonexistent-pkg-xyz-999"]}}}
    with patch("oma_adapter.env_packages._run_pip_install") as mock_install:
        ensure_environment_packages(env)
    mock_install.assert_called_once_with(["oma-nonexistent-pkg-xyz-999"])
