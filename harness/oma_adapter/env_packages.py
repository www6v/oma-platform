"""Install environment.config.packages.pip before harness turns (E1).

Cookbook cloud environments pre-install packages in the sandbox. Locally,
OMA stores ``packages.pip`` on the environment record but must install into
the harness venv before the agent runs ``python3`` via bash.
"""

from __future__ import annotations

import importlib.util
import logging
import os
import subprocess
import sys
from pathlib import Path
from typing import Any

logger = logging.getLogger("oma.env_packages")

# PyPI name → import name for skip-if-present checks.
_IMPORT_NAMES: dict[str, str] = {
    "scikit-learn": "sklearn",
    "pillow": "PIL",
    "pyyaml": "yaml",
}

# Specs successfully installed (or already importable) this process lifetime.
_satisfied_specs: set[str] = set()


def pip_packages_from_environment(
    environment: dict[str, Any] | None,
) -> list[str]:
    """Extract pip package specs from a session environment snapshot."""
    if not environment:
        return []

    packages_block: Any = None
    config = environment.get("config")
    if isinstance(config, dict):
        packages_block = config.get("packages")
    if packages_block is None:
        packages_block = environment.get("packages")

    if isinstance(packages_block, dict):
        pip_list = packages_block.get("pip")
        if isinstance(pip_list, list):
            return _normalize_specs(pip_list)
        return []

    if isinstance(packages_block, list):
        return _normalize_specs(packages_block)

    return []


def harness_venv_bin() -> str:
    """Directory containing the harness interpreter (for PATH in bash)."""
    return str(Path(sys.executable).resolve().parent)


def ensure_environment_packages(
    environment: dict[str, Any] | None,
) -> list[str]:
    """Install missing pip packages declared on the environment snapshot.

    Returns package specs that were installed or already satisfied.
    No-op when ``OMA_SKIP_ENV_PIP=1`` or the snapshot lists no pip packages.
    """
    if os.environ.get("OMA_SKIP_ENV_PIP", "").strip() in ("1", "true", "yes"):
        return []

    specs = pip_packages_from_environment(environment)
    if not specs:
        return []

    pending = [spec for spec in specs if spec not in _satisfied_specs]
    if not pending:
        return list(specs)

    to_install: list[str] = []
    for spec in pending:
        if _spec_importable(spec):
            _satisfied_specs.add(spec)
            continue
        to_install.append(spec)

    if to_install:
        _run_pip_install(to_install)
        for spec in to_install:
            _satisfied_specs.add(spec)

    return specs


def _normalize_specs(raw: list[Any]) -> list[str]:
    out: list[str] = []
    for item in raw:
        if isinstance(item, str):
            text = item.strip()
            if text:
                out.append(text)
    return out


def _pypi_base_name(spec: str) -> str:
    base = spec.split("[", 1)[0]
    for sep in ("==", ">=", "<=", "!=", "~=", ">", "<"):
        if sep in base:
            base = base.split(sep, 1)[0]
            break
    return base.strip().lower()


def _spec_importable(spec: str) -> bool:
    module = _IMPORT_NAMES.get(_pypi_base_name(spec), _pypi_base_name(spec))
    try:
        return importlib.util.find_spec(module) is not None
    except (ImportError, ModuleNotFoundError, ValueError):
        return False


def _ensure_pip() -> None:
    """Bootstrap pip when the harness venv was created by uv without pip."""
    if importlib.util.find_spec("pip") is not None:
        return
    logger.info("[env_packages] bootstrapping pip via ensurepip")
    try:
        subprocess.run(
            [sys.executable, "-m", "ensurepip", "--upgrade"],
            check=True,
            capture_output=True,
            text=True,
        )
    except subprocess.CalledProcessError as exc:
        detail = (exc.stderr or exc.stdout or str(exc)).strip()
        raise RuntimeError(
            "harness venv has no pip and ensurepip failed: "
            f"{detail}"
        ) from exc


def _run_pip_install(specs: list[str]) -> None:
    _ensure_pip()
    timeout = float(os.environ.get("OMA_ENV_PIP_TIMEOUT_SEC", "180"))
    cmd = [sys.executable, "-m", "pip", "install", *specs]
    logger.info("[env_packages] installing %s", specs)
    try:
        subprocess.run(
            cmd,
            check=True,
            capture_output=True,
            text=True,
            timeout=timeout,
        )
    except subprocess.CalledProcessError as exc:
        detail = (exc.stderr or exc.stdout or str(exc)).strip()
        raise RuntimeError(
            f"failed to install environment packages {specs}: {detail}"
        ) from exc
    except subprocess.TimeoutError as exc:
        raise RuntimeError(
            f"timed out after {timeout}s installing environment packages "
            f"{specs}"
        ) from exc
