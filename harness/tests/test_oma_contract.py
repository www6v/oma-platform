"""Golden contract tests aligned with open-managed-agents/test/unit fixtures."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

import pytest

from oma_adapter.emit import emit_oma_events
from oma_adapter.project import project_oma_events

FIXTURES_DIR = Path(__file__).parent / "fixtures" / "oma_contract"


def _load_fixture(name: str) -> dict[str, Any]:
    path = FIXTURES_DIR / name
    return json.loads(path.read_text(encoding="utf-8"))


def _emit_fixture_paths() -> list[Path]:
    return sorted(FIXTURES_DIR.glob("emit_*.json"))


def _project_fixture_paths() -> list[Path]:
    return sorted(FIXTURES_DIR.glob("project_*.json"))


@pytest.mark.parametrize("fixture_path", _emit_fixture_paths(), ids=lambda p: p.name)
def test_emit_oma_events_contract(fixture_path: Path) -> None:
    case = json.loads(fixture_path.read_text(encoding="utf-8"))
    out = emit_oma_events(case["input"])
    assert out == case["expected"]


@pytest.mark.parametrize("fixture_path", _project_fixture_paths(), ids=lambda p: p.name)
def test_project_oma_events_contract(fixture_path: Path) -> None:
    case = json.loads(fixture_path.read_text(encoding="utf-8"))
    assert project_oma_events(case["input"]) == case["expected_prompt"]


def test_user_message_shape_contract() -> None:
    case = _load_fixture("oma_user_message_shape.json")
    event = case["event"]
    assert event["type"] == case["required_fields"]["type"]
    assert event["content"] == case["required_fields"]["content"]


def test_turn_end_message_roundtrip() -> None:
    raw = [
        {
            "type": "turn_end",
            "message": {
                "role": "assistant",
                "content": [{"type": "text", "text": "hello from model"}],
            },
        }
    ]
    out = emit_oma_events(raw)
    assert out[0]["content"][0]["text"] == "hello from model"
