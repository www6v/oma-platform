"""Map OMA model-card providers to piPy model references."""

from __future__ import annotations

PI_PROVIDER_BY_OMA: dict[str, str] = {
    "ant": "anthropic",
    "anthropic": "anthropic",
    "ant-compatible": "anthropic",
    "oai": "openai",
    "openai": "openai",
    "oai-compatible": "openai",
    "faux": "faux",
}


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
    if wire.lower().startswith("claude-"):
        return wire, "anthropic"
    if wire.lower().startswith("gpt-"):
        return wire, "openai"
    return wire, None
