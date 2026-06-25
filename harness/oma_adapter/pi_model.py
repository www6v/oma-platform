"""Map OMA model-card providers to piPy model references."""

from __future__ import annotations

import json
import os
from pathlib import Path

from oma_adapter.types import ModelConfig

PI_PROVIDER_BY_OMA: dict[str, str] = {
    "ant": "anthropic",
    "anthropic": "anthropic",
    "ant-compatible": "anthropic",
    "oai": "openai",
    "openai": "openai",
    "oai-compatible": "openai",
    "dashscope": "dashscope",
    "faux": "faux",
}

_LEGACY_ANTHROPIC_PREFIX = "claude-"


def harness_default_model() -> str:
    """piPy default model: OMA_DEFAULT_MODEL, then ~/.pi/agent/settings.json."""
    explicit = os.environ.get("OMA_DEFAULT_MODEL", "").strip()
    if explicit:
        return explicit
    settings = Path.home() / ".pi" / "agent" / "settings.json"
    if settings.is_file():
        try:
            data = json.loads(settings.read_text(encoding="utf-8"))
            name = (data.get("defaultModel") or "").strip()
            if name:
                return name
        except (OSError, json.JSONDecodeError):
            pass
    return "qwen3.7-plus"


def is_legacy_anthropic_model(model_id: str) -> bool:
    lower = (model_id or "").strip().lower()
    return lower.startswith(_LEGACY_ANTHROPIC_PREFIX)


def apply_pipy_model_override(
    *,
    model: ModelConfig | None,
    agent_model: str,
) -> ModelConfig | None:
    """Remap legacy Claude models to piPy default (dashscope + local auth)."""
    raw = (model.model if model is not None else agent_model).strip()
    if not is_legacy_anthropic_model(raw):
        return model

    target = harness_default_model()
    if is_legacy_anthropic_model(target):
        return model

    return ModelConfig(model=target, provider="dashscope")


def normalize_harness_models(
    model: ModelConfig | None,
    *,
    agent_model: str,
    aux_model: ModelConfig | None = None,
) -> tuple[ModelConfig | None, ModelConfig | None]:
    """Apply piPy default model override to primary and aux configs."""
    primary = apply_pipy_model_override(model=model, agent_model=agent_model)
    aux = None
    if aux_model is not None:
        aux = apply_pipy_model_override(
            model=aux_model,
            agent_model=aux_model.model,
        )
    return primary, aux


def normalize_pi_provider(oma_provider: str | None) -> str | None:
    """Translate OMA API-compat tags to pi_ai provider ids."""
    if not oma_provider:
        return None
    return PI_PROVIDER_BY_OMA.get(oma_provider.strip().lower(), oma_provider.strip())


def resolve_session_model_pattern(
    *,
    wire_model: str,
    oma_provider: str | None = None,
) -> tuple[str, str | None]:
    """Return (model_pattern, provider_override) for create_agent_session."""
    wire = wire_model.strip()
    if not wire:
        return wire, None
    if "/" in wire:
        prefix, _, suffix = wire.partition("/")
        if suffix:
            mapped = normalize_pi_provider(prefix)
            if mapped and mapped != prefix:
                return f"{mapped}/{suffix}", None
        return wire, None

    pi_provider = normalize_pi_provider(oma_provider)
    if pi_provider:
        return wire, pi_provider
    lower = wire.lower()
    if lower.startswith("qwen"):
        return wire, "dashscope"
    if lower.startswith("claude-"):
        return wire, "anthropic"
    if lower.startswith("gpt-"):
        return wire, "openai"
    return wire, None
