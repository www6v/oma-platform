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
    "litellm": "litellm",
    "faux": "faux",
}

_LEGACY_ANTHROPIC_PREFIX = "claude-"
_OPENAI_NATIVE_PREFIXES = (
    "gpt-",
    "o1",
    "o3",
    "o4",
    "chatgpt-",
    "text-embedding-",
    "whisper-",
    "dall-e",
    "tts-",
)


def _pi_settings_path() -> Path:
    return Path.home() / ".pi" / "agent" / "settings.json"


def _load_pi_settings() -> dict:
    settings = _pi_settings_path()
    if not settings.is_file():
        return {}
    try:
        data = json.loads(settings.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        return {}
    return data if isinstance(data, dict) else {}


def harness_default_model() -> str:
    """piPy default model: OMA_DEFAULT_MODEL, then ~/.pi/agent/settings.json."""
    explicit = os.environ.get("OMA_DEFAULT_MODEL", "").strip()
    if explicit:
        return explicit
    name = (_load_pi_settings().get("defaultModel") or "").strip()
    if name:
        return name
    return "qwen3.7-plus"


def harness_default_provider() -> str | None:
    """piPy default provider: OMA_DEFAULT_PROVIDER, then settings.json."""
    explicit = os.environ.get("OMA_DEFAULT_PROVIDER", "").strip()
    if explicit:
        return explicit
    name = (_load_pi_settings().get("defaultProvider") or "").strip()
    return name or None


def is_legacy_anthropic_model(model_id: str) -> bool:
    lower = (model_id or "").strip().lower()
    return lower.startswith(_LEGACY_ANTHROPIC_PREFIX)


def looks_like_openai_native_model(model_id: str) -> bool:
    """True when the wire id is a first-party OpenAI model name."""
    bare = (model_id or "").strip().lower()
    if "/" in bare:
        bare = bare.rsplit("/", 1)[-1]
    return any(bare.startswith(prefix) for prefix in _OPENAI_NATIVE_PREFIXES)


def apply_pipy_model_override(
    *,
    model: ModelConfig | None,
    agent_model: str,
) -> ModelConfig | None:
    """Remap legacy Claude / mis-tagged oai-compatible cards to pi defaults."""
    raw = (model.model if model is not None else agent_model).strip()
    preferred = harness_default_provider() or "dashscope"

    if is_legacy_anthropic_model(raw):
        target = harness_default_model()
        if is_legacy_anthropic_model(target):
            return model
        return ModelConfig(model=target, provider=preferred)

    if model is None:
        return None

    mapped = normalize_pi_provider(model.provider)
    # OMA "oai-compatible" cards often wrap third-party models that live under a
    # custom models.json provider (litellm, dashscope, …). Mapping them to
    # openai makes piPy raise Unknown model: openai/<id>. Prefer ~/.pi default.
    if (
        mapped == "openai"
        and not looks_like_openai_native_model(model.model)
        and preferred
        and preferred != "openai"
    ):
        return ModelConfig(model=model.model, provider=preferred)

    return model


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
    preferred = harness_default_provider()
    if pi_provider == "openai" and not looks_like_openai_native_model(wire):
        if preferred and preferred != "openai":
            return wire, preferred
    if pi_provider:
        return wire, pi_provider
    lower = wire.lower()
    if lower.startswith("qwen"):
        return wire, preferred or "dashscope"
    if lower.startswith("claude-"):
        return wire, "anthropic"
    if looks_like_openai_native_model(wire):
        return wire, "openai"
    if preferred:
        return wire, preferred
    return wire, None
