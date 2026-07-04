"""Explore unfamiliar codebase cookbook tests."""

from __future__ import annotations

import os
import sys
import zipfile
from pathlib import Path

import pytest

_EXAMPLE7 = Path(__file__).resolve().parents[1] / "example" / "example7"
if str(_EXAMPLE7) not in sys.path:
    sys.path.insert(0, str(_EXAMPLE7))

from explore_fixtures import (  # noqa: E402
    DEPLOY_HISTORY_BYTES,
    REPO_MOUNT_PATH,
    REPO_UPLOAD_PATH,
    build_repo_resource,
    make_unfamiliar_repo_zip,
)

_RUN_LIVE = os.getenv("OMA_RUN_LIVE_EXPLORE", "0") == "1"


@pytest.fixture
async def oma_client():
    from oma_sdk import OMAClient

    client = OMAClient(base_url=os.getenv("OMA_BASE_URL", "http://localhost:8787"))
    yield client
    await client.aclose()


def test_repo_zip_has_services_layout() -> None:
    buf = make_unfamiliar_repo_zip()
    with zipfile.ZipFile(buf) as zf:
        names = zf.namelist()
    assert "ARCHITECTURE.md" in names
    assert "services/auth/main.py" in names
    assert "services/widgets/main.py" in names


def test_repo_resource_mount_path() -> None:
    resource = build_repo_resource("file_test")
    assert resource["mount_path"] == REPO_MOUNT_PATH
    assert resource["type"] == "file"


def test_explore_user_message_mentions_upload_path() -> None:
    from explore_fixtures import EXPLORE_USER_MESSAGE

    assert REPO_UPLOAD_PATH in EXPLORE_USER_MESSAGE


def test_deploy_history_fixture() -> None:
    assert b"microservices" in DEPLOY_HISTORY_BYTES


@pytest.mark.live
@pytest.mark.asyncio
@pytest.mark.skipif(
    not _RUN_LIVE,
    reason="set OMA_RUN_LIVE_EXPLORE=1 for live explore soak",
)
async def test_explore_unfamiliar_codebase_live_soak(oma_client) -> None:
    from explore_unfamiliar_codebase_soak import run_explore_unfamiliar_codebase_soak

    model = os.getenv("OMA_MODEL", "qwen3.7-plus")
    result = await run_explore_unfamiliar_codebase_soak(
        oma_client,
        model=model,
        keep_resources=os.getenv("OMA_KEEP_RESOURCES") == "1",
    )
    assert result["resource_count_after_add"] >= 2
    assert result["resource_count_after_delete"] == 1
