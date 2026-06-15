"""Tests for OMA → piPy provider mapping."""

from __future__ import annotations

from oma_adapter.pi_model import (
    normalize_pi_provider,
    resolve_session_model_pattern,
)


def test_normalize_pi_provider_maps_oma_tags() -> None:
    assert normalize_pi_provider("ant") == "anthropic"
    assert normalize_pi_provider("ant-compatible") == "anthropic"
    assert normalize_pi_provider("oai") == "openai"
    assert normalize_pi_provider("oai-compatible") == "openai"


def test_resolve_session_model_pattern_for_ant_compatible() -> None:
    model, provider = resolve_session_model_pattern(
        wire_model="claude-sonnet-4-6",
        oma_provider="ant-compatible",
    )
    assert model == "claude-sonnet-4-6"
    assert provider == "anthropic"


def test_resolve_session_model_pattern_for_bare_claude() -> None:
    model, provider = resolve_session_model_pattern(
        wire_model="claude-sonnet-4-6",
        oma_provider=None,
    )
    assert model == "claude-sonnet-4-6"
    assert provider == "anthropic"


def test_resolve_session_model_pattern_keeps_qualified_model() -> None:
    model, provider = resolve_session_model_pattern(
        wire_model="faux/test",
        oma_provider=None,
    )
    assert model == "faux/test"
    assert provider is None
