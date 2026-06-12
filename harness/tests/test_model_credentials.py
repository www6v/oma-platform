"""Tests for model credential resolution."""

from __future__ import annotations

import os
from contextlib import contextmanager

from oma_adapter.provider_env import provider_env
from oma_adapter.types import ModelConfig


@contextmanager
def _isolated_env(*keys: str):
    saved = {key: os.environ.get(key) for key in keys}
    try:
        yield
    finally:
        for key, value in saved.items():
            if value is None:
                os.environ.pop(key, None)
            else:
                os.environ[key] = value


def test_provider_env_sets_anthropic_key_and_base_url() -> None:
    model = ModelConfig(
        model="claude-sonnet-4-20250514",
        provider="ant",
        api_key="sk-test",
        base_url="https://proxy.example.test",
    )
    with _isolated_env("ANTHROPIC_API_KEY", "ANTHROPIC_BASE_URL"):
        os.environ.pop("ANTHROPIC_API_KEY", None)
        os.environ.pop("ANTHROPIC_BASE_URL", None)
        with provider_env(model):
            assert os.environ["ANTHROPIC_API_KEY"] == "sk-test"
            assert os.environ["ANTHROPIC_BASE_URL"] == "https://proxy.example.test"
        assert "ANTHROPIC_API_KEY" not in os.environ
        assert "ANTHROPIC_BASE_URL" not in os.environ


def test_provider_env_sets_openai_key_and_base_url() -> None:
    model = ModelConfig(
        model="gpt-4.1",
        provider="oai",
        api_key="sk-oai",
        base_url="https://oai.example.test",
    )
    with _isolated_env("OPENAI_API_KEY", "OPENAI_BASE_URL", "ANTHROPIC_API_KEY"):
        os.environ.pop("OPENAI_API_KEY", None)
        os.environ.pop("OPENAI_BASE_URL", None)
        with provider_env(model):
            assert os.environ["OPENAI_API_KEY"] == "sk-oai"
            assert os.environ["OPENAI_BASE_URL"] == "https://oai.example.test"
        assert "OPENAI_API_KEY" not in os.environ
