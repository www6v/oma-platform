"""
Environment Examples - High-level helper functions for environment operations.
"""

from __future__ import annotations

import os
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    import anthropic

_KEEP = os.getenv("OMA_KEEP_RESOURCES", "0") == "1"


class EnvironmentExamples:
    """Example operations for environments."""

    @staticmethod
    def create_and_retrieve(client: anthropic.Anthropic, name: str = "sdk-e2e-env-create") -> dict:
        """
        Create an environment and retrieve it to verify.
        
        Args:
            client: Anthropic client instance
            name: Name for the environment
            
        Returns:
            Dictionary with environment details
        """
        env = client.beta.environments.create(name=name)
        try:
            assert env.id
            got = client.beta.environments.retrieve(env.id)
            assert got.id == env.id
            assert got.name == env.name
            return {"environment": env, "retrieved": got}
        finally:
            if not _KEEP:
                client.beta.environments.archive(env.id)
            else:
                print(f"\n[KEEP] environment {env.id} ({name}) — archive manually when done")

    @staticmethod
    def list_environments(client: anthropic.Anthropic) -> list:
        """
        List all environments.
        
        Args:
            client: Anthropic client instance
            
        Returns:
            List of environments
        """
        page = client.beta.environments.list()
        envs = list(page)
        assert isinstance(envs, list)
        assert len(envs) >= 1  # at least the default env exists
        return envs

    @staticmethod
    def update_environment(
        client: anthropic.Anthropic,
        name_before: str = "sdk-e2e-env-before",
        name_after: str = "sdk-e2e-env-after"
    ) -> dict:
        """
        Create an environment, update it, and verify.
        
        Args:
            client: Anthropic client instance
            name_before: Initial name for the environment
            name_after: Updated name for the environment
            
        Returns:
            Dictionary with environment details
        """
        env = client.beta.environments.create(name=name_before)
        try:
            updated = client.beta.environments.update(env.id, name=name_after)
            assert updated.name == name_after
            return {"environment": env, "updated": updated}
        finally:
            if not _KEEP:
                client.beta.environments.archive(env.id)
            else:
                print(f"\n[KEEP] environment {env.id} ({name_after}) — archive manually when done")

    @staticmethod
    def delete_environment(client: anthropic.Anthropic, name: str = "sdk-e2e-env-delete") -> dict:
        """Create an environment and hard-delete it."""
        env = client.beta.environments.create(name=name)
        deleted = client.beta.environments.delete(env.id)
        assert deleted.type == "environment_deleted"
        return {"environment": env, "deleted": deleted}

    @staticmethod
    def archive_environment(client: anthropic.Anthropic, name: str = "sdk-e2e-env-archive") -> dict:
        """
        Create an environment and archive it.

        Args:
            client: Anthropic client instance
            name: Name for the environment

        Returns:
            Dictionary with environment details
        """
        env = client.beta.environments.create(name=name)
        archived = client.beta.environments.archive(env.id)
        assert archived.id == env.id
        return {"environment": env, "archived": archived}

    # ---- sandbox config helpers (per-environment sandbox binding) --------
    #
    # The server accepts a JSON `config` field on POST/PATCH /v1/environments.
    # The Anthropic SDK's typed `config=` parameter only knows about `cloud` /
    # `self_hosted` configs, so we pass our custom shapes (e.g.
    # `{"type":"sandbox","sandbox":{...}}`) as untyped dicts — the SDK
    # serialises whatever dict we hand it at runtime. `# type: ignore` keeps
    # static type-checkers quiet.

    @staticmethod
    def create_with_local_config(
        client: anthropic.Anthropic,
        name: str = "sdk-e2e-env-local",
    ) -> dict:
        """
        Create an environment with an explicit ``{"type":"local"}`` config.

        This pins the environment to the local provider — sessions bound to
        it will run on the host workdir even when the deployment's global
        ``SANDBOX_PROVIDER`` points at a remote provider.
        """
        env = client.beta.environments.create(
            name=name,
            config={"type": "local"},  # type: ignore[arg-type]
        )
        try:
            assert env.id
            got = client.beta.environments.retrieve(env.id)
            assert got.id == env.id
            return {"environment": env, "retrieved": got}
        finally:
            if not _KEEP:
                client.beta.environments.archive(env.id)
            else:
                print(f"\n[KEEP] environment {env.id} ({name}) — archive manually when done")

    @staticmethod
    def create_with_sandbox_config(
        client: anthropic.Anthropic,
        name: str = "sdk-e2e-env-sandbox",
        *,
        image: str = "python:3.12-slim",
        cpu: str | None = None,
        memory: str | None = None,
        domain: str | None = None,
        execd_port: int | None = None,
        timeout_seconds: int | None = None,
    ) -> dict:
        """
        Create an environment bound to OpenSandbox with an env-specific config.

        Missing fields inherit from the deployment's global OpenSandbox
        config, so callers typically only need to set ``image`` to pin a
        session to a particular container image.
        """
        opensandbox: dict = {"image": image}
        if cpu is not None:
            opensandbox["cpu"] = cpu
        if memory is not None:
            opensandbox["memory"] = memory
        if domain is not None:
            opensandbox["domain"] = domain
        if execd_port is not None:
            opensandbox["execd_port"] = execd_port
        if timeout_seconds is not None:
            opensandbox["timeout_seconds"] = timeout_seconds

        config = {
            "type": "sandbox",
            "sandbox": {
                "provider": "opensandbox",
                "opensandbox": opensandbox,
            },
        }
        env = client.beta.environments.create(
            name=name,
            config=config,  # type: ignore[arg-type]
        )
        try:
            assert env.id
            got = client.beta.environments.retrieve(env.id)
            assert got.id == env.id
            return {"environment": env, "retrieved": got, "config": config}
        finally:
            if not _KEEP:
                client.beta.environments.archive(env.id)
            else:
                print(f"\n[KEEP] environment {env.id} ({name}) — archive manually when done")

    @staticmethod
    def update_with_config(
        client: anthropic.Anthropic,
        env_id: str,
        config: dict,
    ) -> dict:
        """
        Update an environment's config and return the updated record.

        ``config`` is a raw dict shaped like the POST body's ``config``
        field (e.g. ``{"type":"sandbox","sandbox":{"provider":"opensandbox",
        "opensandbox":{"image":"..."}}}``).
        """
        updated = client.beta.environments.update(
            env_id,
            config=config,  # type: ignore[arg-type]
        )
        return {"updated": updated, "config": config}

    @staticmethod
    def try_create_with_malformed_config(
        client: anthropic.Anthropic,
        name: str = "sdk-e2e-env-bad",
        config: dict | None = None,
    ):
        """
        Attempt to create an environment with a malformed config.

        Returns the raised exception (caller asserts on its type/message).
        The environment is NOT created on the server; nothing to clean up.
        """
        if config is None:
            # Default bad config: wrong JSON type for a known numeric field.
            config = {
                "type": "sandbox",
                "sandbox": {
                    "provider": "opensandbox",
                    "opensandbox": {"execd_port": "not-a-number"},
                },
            }
        try:
            client.beta.environments.create(
                name=name,
                config=config,  # type: ignore[arg-type]
            )
        except Exception as exc:  # noqa: BLE001 — we want to return whatever the SDK raises
            return exc
        return None  # no error raised → test should fail

    @staticmethod
    def bind_session_and_exec(
        client: anthropic.Anthropic,
        env_id: str,
        agent_id: str,
        command: str = "echo sdk-env-binding-ok && uname -s",
        timeout_ms: int = 30000,
    ) -> dict:
        """
        Create a session bound to ``env_id``, run a command via
        ``POST /v1/sessions/:id/exec``, and return the result.

        This is the simplest way to verify that a per-env sandbox config
        is actually honoured: if the env pins image X, the exec runs
        inside a container built from X. The Anthropic SDK doesn't yet
        expose a typed ``sessions.exec`` wrapper, so we drop to httpx.
        """
        import httpx

        from .sessions import SessionExamples

        sess = SessionExamples._create_session(client, agent_id, env_id)
        base_url = str(client.base_url).rstrip("/")
        # The server accepts either `x-api-key` (native oma auth) or
        # `Authorization: Bearer` (anthropic SDK wire format). Send both
        # so this helper works against both upstream and oma-platform
        # deployments.
        headers = {
            "x-api-key": client.api_key,
            "Authorization": f"Bearer {client.api_key}",
            "Content-Type": "application/json",
        }
        try:
            resp = httpx.post(
                f"{base_url}/v1/sessions/{sess.id}/exec",
                headers=headers,
                json={"command": command, "timeout_ms": timeout_ms},
                timeout=timeout_ms / 1000 + 10,
            )
            resp.raise_for_status()
            body = resp.json()
            return {"session": sess, "output": body.get("output", "")}
        finally:
            if not _KEEP:
                try:
                    client.beta.sessions.archive(sess.id)
                except Exception:
                    pass
            else:
                print(f"\n[KEEP] session {sess.id} — archive manually when done")

