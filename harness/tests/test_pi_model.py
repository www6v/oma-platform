"""Tests for OMA → piPy provider mapping."""

from __future__ import annotations

import os
from contextlib import contextmanager

from oma_adapter.pi_model import (
    apply_pipy_model_override,
    normalize_pi_provider,
    resolve_session_model_pattern,
)
from oma_adapter.turn import _should_use_fake_harness
from oma_adapter.types import ModelConfig


@contextmanager
def _env(key: str, value: str | None):
    saved = os.environ.get(key)
    try:
        if value is None:
            os.environ.pop(key, None)
        else:
            os.environ[key] = value
        yield
    finally:
        if saved is None:
            os.environ.pop(key, None)
        else:
            os.environ[key] = saved


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


def test_resolve_session_model_pattern_for_qwen() -> None:
    model, provider = resolve_session_model_pattern(
        wire_model="qwen3.7-plus",
        oma_provider=None,
    )
    assert model == "qwen3.7-plus"
    assert provider == "dashscope"


def test_resolve_session_model_pattern_keeps_qualified_model() -> None:
    model, provider = resolve_session_model_pattern(
        wire_model="faux/test",
        oma_provider=None,
    )
    assert model == "faux/test"
    assert provider is None


def test_apply_pipy_model_override_remaps_claude() -> None:
    cfg = ModelConfig(
        model="claude-sonnet-4-6",
        provider="ant-compatible",
        api_key="sk-stale",
        base_url="https://ai.anta.com/aimodels-server/private/llm",
    )
    with _env("OMA_DEFAULT_MODEL", "qwen3.7-plus"):
        out = apply_pipy_model_override(model=cfg, agent_model="claude-sonnet-4-6")
    assert out is not None
    assert out.model == "qwen3.7-plus"
    assert out.provider == "dashscope"
    assert out.api_key is None


def test_apply_pipy_model_override_keeps_qwen() -> None:
    cfg = ModelConfig(model="qwen3.7-plus", provider="dashscope")
    out = apply_pipy_model_override(model=cfg, agent_model="qwen3.7-plus")
    assert out is cfg


def test_fake_harness_skipped_when_model_card_has_api_key() -> None:
    cfg = ModelConfig(
        model="claude-sonnet-4-6",
        provider="ant-compatible",
        api_key="sk-test",
        base_url="https://proxy.example.test",
    )
    with _env("OMA_FAKE_HARNESS", "1"):
        assert not _should_use_fake_harness(model=cfg, wire_model=cfg.model)


def test_fake_harness_used_without_credentials() -> None:
    with _env("OMA_FAKE_HARNESS", "1"):
        assert _should_use_fake_harness(
            model=None,
            wire_model="claude-sonnet-4-6",
        )
