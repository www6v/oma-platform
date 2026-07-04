"""Orchestrate issue to PR cookbook tests."""

from __future__ import annotations

import os
import sys
import zipfile
from pathlib import Path

import pytest

_EXAMPLE8 = Path(__file__).resolve().parents[1] / "example" / "example8"
if str(_EXAMPLE8) not in sys.path:
    sys.path.insert(0, str(_EXAMPLE8))

from orchestrate_fixtures import (  # noqa: E402
    ENV_CONFIG,
    PR_STATE_PATH,
    REPO_MOUNT_PATH,
    REPO_UPLOAD_PATH,
    build_repo_resource,
    make_orchestrate_repo_zip,
)

_RUN_LIVE = os.getenv("OMA_RUN_LIVE_ORCHESTRATE", "0") == "1"


@pytest.fixture
async def oma_client():
    from oma_sdk import OMAClient

    client = OMAClient(base_url=os.getenv("OMA_BASE_URL", "http://localhost:8787"))
    yield client
    await client.aclose()


def test_orchestrate_zip_has_mock_gh_and_tests() -> None:
    buf = make_orchestrate_repo_zip()
    with zipfile.ZipFile(buf) as zf:
        names = zf.namelist()
    assert "gh-mock" in names
    assert "issue_42.json" in names
    assert "src/url_utils.py" in names
    assert "tests/test_urls.py" in names


def test_repo_resource_mount_path() -> None:
    resource = build_repo_resource("file_test")
    assert resource["mount_path"] == REPO_MOUNT_PATH


def test_env_config_includes_pytest() -> None:
    assert "pytest" in ENV_CONFIG["packages"]["pip"]
    assert ENV_CONFIG["networking"]["allow_package_managers"] is True


def test_chain_message_mentions_upload_path() -> None:
    from orchestrate_fixtures import CHAIN_USER_MESSAGE

    assert REPO_UPLOAD_PATH in CHAIN_USER_MESSAGE


def test_verify_message_mentions_pr_state() -> None:
    from orchestrate_fixtures import VERIFY_USER_MESSAGE

    assert PR_STATE_PATH in VERIFY_USER_MESSAGE


@pytest.mark.live
@pytest.mark.asyncio
@pytest.mark.skipif(
    not _RUN_LIVE,
    reason="set OMA_RUN_LIVE_ORCHESTRATE=1 for live orchestrate soak",
)
async def test_orchestrate_issue_to_pr_live_soak(oma_client) -> None:
    from orchestrate_issue_to_pr_soak import run_orchestrate_issue_to_pr_soak

    model = os.getenv("OMA_MODEL", "qwen3.7-plus")
    result = await run_orchestrate_issue_to_pr_soak(
        oma_client,
        model=model,
        keep_resources=os.getenv("OMA_KEEP_RESOURCES") == "1",
    )
    assert result["turn_count"] == 2
