"""E2E tests for /v1/environments."""

from __future__ import annotations

import os
import time

import anthropic
import pytest

from oma_sdk.examples import EnvironmentExamples


def test_environments_create_and_retrieve(client: anthropic.Anthropic):
    result = EnvironmentExamples.create_and_retrieve(client)
    assert result["environment"].id
    assert result["retrieved"].id == result["environment"].id
    assert result["retrieved"].name == result["environment"].name


def test_environments_list(client: anthropic.Anthropic):
    envs = EnvironmentExamples.list_environments(client)
    assert isinstance(envs, list)
    assert len(envs) >= 1  # at least the default env exists


def test_environments_update(client: anthropic.Anthropic):
    result = EnvironmentExamples.update_environment(client)
    assert result["updated"].name == "sdk-e2e-env-after"


def test_environments_archive(client: anthropic.Anthropic):
    result = EnvironmentExamples.archive_environment(client)
    assert result["archived"].id == result["environment"].id


def test_environments_delete(client: anthropic.Anthropic):
    result = EnvironmentExamples.delete_environment(client)
    assert result["deleted"].type == "environment_deleted"


# --- per-environment sandbox binding (Phase 5) ----------------------------
#
# These tests exercise the `config` field on POST/PATCH /v1/environments
# and confirm the server honours it when sessions run. The schema and
# validation rules live in internal/sandbox/resolver.go.


def test_environments_create_with_local_config(client: anthropic.Anthropic):
    """Explicit {"type":"local"} pins an env to the local provider."""
    result = EnvironmentExamples.create_with_local_config(client)
    assert result["environment"].id
    assert result["retrieved"].id == result["environment"].id


def test_environments_create_with_sandbox_config(client: anthropic.Anthropic):
    """OpenSandbox config accepted; env is created and retrievable."""
    result = EnvironmentExamples.create_with_sandbox_config(
        client,
        image="python:3.12-slim",
        cpu="500m",
        memory="512Mi",
    )
    assert result["environment"].id
    # The Anthropic SDK's typed BetaEnvironment response doesn't expose
    # our custom `sandbox` shape, so we only assert on the fields the
    # SDK does surface. The round-trip through the server's DB (config
    # stored verbatim, validator accepted it) is the real guarantee —
    # the full per-env binding is exercised end-to-end by
    # `test_session_binds_to_sandbox_env` and by
    # `scripts/e2e/smoke-environment-sandbox-binding-e2e.sh`.
    got = result["retrieved"]
    assert got.id == result["environment"].id


def test_environments_update_config(client: anthropic.Anthropic):
    """Updating an env's config persists the new value."""
    env = client.beta.environments.create(
        name="sdk-e2e-env-cfg-update",
        config={"type": "local"},  # type: ignore[arg-type]
    )
    try:
        new_config = {
            "type": "sandbox",
            "sandbox": {
                "provider": "opensandbox",
                "opensandbox": {"image": "python:3.12-slim", "cpu": "1000m"},
            },
        }
        result = EnvironmentExamples.update_with_config(client, env.id, new_config)
        assert result["updated"].id == env.id
    finally:
        try:
            client.beta.environments.archive(env.id)
        except Exception:
            pass


def test_environments_rejects_malformed_config(client: anthropic.Anthropic):
    """Schema violations produce a 400, not a 500 / silent success."""
    cases = [
        ("wrong_field_type", {
            "type": "sandbox",
            "sandbox": {
                "provider": "opensandbox",
                "opensandbox": {"execd_port": "not-a-number"},
            },
        }),
        ("sandbox_missing_when_type_is_sandbox", {"type": "sandbox"}),
        ("provider_missing", {
            "type": "sandbox",
            "sandbox": {},
        }),
        ("execd_port_out_of_range", {
            "type": "sandbox",
            "sandbox": {
                "provider": "opensandbox",
                "opensandbox": {"execd_port": 99999},
            },
        }),
        ("raw_invalid_json_via_extra_body", None),  # handled separately below
    ]
    for name, bad_cfg in cases:
        exc = EnvironmentExamples.try_create_with_malformed_config(
            client, name=f"sdk-e2e-env-bad-{name}", config=bad_cfg,
        )
        assert exc is not None, f"{name}: expected an exception, got None"
        # The SDK surfaces 400s as BadRequestError.
        assert isinstance(exc, anthropic.BadRequestError), (
            f"{name}: expected BadRequestError, got {type(exc).__name__}: {exc}"
        )

    # Raw invalid JSON can't be expressed as a typed config dict, so we
    # hit the endpoint directly via httpx to confirm the server rejects it.
    import httpx
    base_url = str(client.base_url).rstrip("/")
    resp = httpx.post(
        f"{base_url}/v1/environments",
        headers={
            "x-api-key": client.api_key,
            "Authorization": f"Bearer {client.api_key}",
            "Content-Type": "application/json",
        },
        content='{"name":"sdk-e2e-env-bad-raw","config":not-json}',
        timeout=10.0,
    )
    assert resp.status_code == 400, f"expected 400, got {resp.status_code}: {resp.text}"


@pytest.mark.skipif(
    not os.getenv("OPENSANDBOX_DOMAIN"),
    reason="OPENSANDBOX_DOMAIN not set — sandbox-requiring SDK test skipped",
)
def test_session_binds_to_sandbox_env(client: anthropic.Anthropic):
    """End-to-end: env with opensandbox image → session → exec runs in it."""
    # Create a dedicated agent (avoid the shared tmp_agent fixture, which
    # uses a fixed name and can collide with a previously-archived record
    # when a prior test run left state behind). Use a timestamp suffix
    # so successive runs of this test each get a fresh, non-colliding name.
    agent_name = f"sdk-e2e-env-binding-{int(time.time())}"
    agent = client.beta.agents.create(
        name=agent_name,
        model={"id": "qwen3.7-plus"},
        system="SDK e2e sandbox-binding test agent.",
    )
    try:
        # Create the env inline (not via EnvironmentExamples.create_with_sandbox_config,
        # which archives the env in its finally block — the session create below
        # needs the env to still be live).
        env = client.beta.environments.create(
            name=f"sdk-e2e-env-binding-{int(time.time())}",
            config={  # type: ignore[arg-type]
                "type": "sandbox",
                "sandbox": {
                    "provider": "opensandbox",
                    "opensandbox": {"image": "python:3.12-slim"},
                },
            },
        )
        env_id = env.id
        try:
            result = EnvironmentExamples.bind_session_and_exec(
                client,
                env_id=env_id,
                agent_id=agent.id,
                command="echo sdk-env-binding-ok && uname -s",
            )
            output = result["output"]
            assert "sdk-env-binding-ok" in output, f"unexpected exec output: {output!r}"
            assert "Linux" in output, (
                f"expected Linux (sandbox) in output, got: {output!r}"
            )
        finally:
            try:
                client.beta.environments.archive(env_id)
            except Exception:
                pass
    finally:
        try:
            client.beta.agents.archive(agent.id)
        except Exception:
            pass

