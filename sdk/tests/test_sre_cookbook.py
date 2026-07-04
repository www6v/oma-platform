"""SRE incident responder cookbook tests."""

from __future__ import annotations

import json
import os
import sys
from pathlib import Path

import pytest

_EXAMPLE9 = Path(__file__).resolve().parents[1] / "example" / "example9"
if str(_EXAMPLE9) not in sys.path:
    sys.path.insert(0, str(_EXAMPLE9))

from sre_fixtures import (  # noqa: E402
    ENV_CONFIG,
    LOG_MOUNT,
    MANIFEST_MOUNT,
    RUNBOOK_MOUNT,
    build_session_resources,
    fixture_path,
    load_alert_json,
)

_RUN_LIVE = os.getenv("OMA_RUN_LIVE_SRE", "0") == "1"


@pytest.fixture
async def oma_client():
    from oma_sdk import OMAClient

    client = OMAClient(base_url=os.getenv("OMA_BASE_URL", "http://localhost:8787"))
    yield client
    await client.aclose()


def test_sre_fixtures_exist() -> None:
    assert fixture_path("alert.json").is_file()
    assert fixture_path("logs/checkout-svc.log").is_file()
    assert fixture_path("infra/k8s/checkout-deploy.yaml").is_file()
    assert fixture_path("runbooks/oom.md").is_file()


def test_alert_json_is_pagerduty_incident() -> None:
    payload = json.loads(load_alert_json())
    assert payload["event"]["event_type"] == "incident.triggered"
    assert "checkout-svc" in payload["event"]["data"]["title"]


def test_session_resources_mount_paths() -> None:
    resources = build_session_resources("f1", "f2", "f3")
    paths = {r["mount_path"] for r in resources}
    assert paths == {LOG_MOUNT, MANIFEST_MOUNT, RUNBOOK_MOUNT}


def test_manifest_has_128mi_trap() -> None:
    text = fixture_path("infra/k8s/checkout-deploy.yaml").read_text(encoding="utf-8")
    assert "128Mi" in text


def test_runbook_mentions_oom() -> None:
    text = fixture_path("runbooks/oom.md").read_text(encoding="utf-8")
    assert "OOMKilled" in text


def test_env_config_is_limited_network() -> None:
    assert ENV_CONFIG["networking"]["type"] == "limited"


@pytest.mark.live
@pytest.mark.asyncio
@pytest.mark.skipif(
    not _RUN_LIVE,
    reason="set OMA_RUN_LIVE_SRE=1 for live SRE soak",
)
async def test_sre_incident_responder_live_soak(oma_client) -> None:
    from sre_incident_responder_soak import run_sre_incident_responder_soak

    model = os.getenv("OMA_MODEL", "qwen3.7-plus")
    result = await run_sre_incident_responder_soak(
        oma_client,
        model=model,
        keep_resources=os.getenv("OMA_KEEP_RESOURCES") == "1",
    )
    assert result["session_id"]
    assert len(result["responded_ids"]) >= 1
