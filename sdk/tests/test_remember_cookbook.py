"""Remember preferences cookbook tests."""

from __future__ import annotations

import os
import sys
from pathlib import Path

import pytest

_EXAMPLE6 = Path(__file__).resolve().parents[1] / "example" / "example6"
if str(_EXAMPLE6) not in sys.path:
    sys.path.insert(0, str(_EXAMPLE6))

from remember_fixtures import (  # noqa: E402
    MEMORY_INSTRUCTIONS,
    PREFERENCE_MOUNT,
    PREFERENCE_PATH,
    build_memory_resource,
)

_RUN_LIVE = os.getenv("OMA_RUN_LIVE_REMEMBER", "0") == "1"


@pytest.fixture
async def oma_client():
    from oma_sdk import OMAClient

    client = OMAClient(base_url=os.getenv("OMA_BASE_URL", "http://localhost:8787"))
    yield client
    await client.aclose()


def test_preference_mount_path() -> None:
    assert PREFERENCE_MOUNT.startswith("/mnt/memory/")
    assert PREFERENCE_PATH in PREFERENCE_MOUNT


def test_memory_resource_includes_instructions() -> None:
    resource = build_memory_resource("mst_test")
    assert resource["type"] == "memory_store"
    assert resource["instructions"] == MEMORY_INSTRUCTIONS
    assert PREFERENCE_MOUNT in resource["instructions"]


@pytest.mark.live
@pytest.mark.asyncio
@pytest.mark.skipif(
    not _RUN_LIVE,
    reason="set OMA_RUN_LIVE_REMEMBER=1 for live remember soak",
)
async def test_remember_preferences_live_soak(oma_client) -> None:
    from remember_preferences_soak import run_remember_preferences_soak

    model = os.getenv("OMA_MODEL", "qwen3.7-plus")
    result = await run_remember_preferences_soak(
        oma_client,
        model=model,
        keep_resources=os.getenv("OMA_KEEP_RESOURCES") == "1",
    )
    assert result["saved_content"]
    assert result["cross_session_recall"] is True
    assert "bullet" in (result["recalled_content"] or "").lower()
