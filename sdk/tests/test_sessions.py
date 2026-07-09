"""E2E tests for /v1/sessions — CRUD, events, archive, delete.

Two tests, two session creations per run.  The server rate-limits session
creation to 5 per 60 s; four separate tests (4 creations/run) exhausts the
budget on back-to-back runs.  Combining into two tests keeps each double-run
at 4 creations total, safely under the limit.
"""

from __future__ import annotations

import anthropic

from oma_sdk.examples import SessionExamples, VaultExamples


def test_sessions_crud_and_events(client: anthropic.Anthropic, tmp_agent, tmp_env):
    """Create → retrieve → list → send event → list events, all on one session."""
    result = SessionExamples.crud_and_events(client, tmp_agent.id, tmp_env)
    assert result["session"].id
    assert result["retrieved"].id == result["session"].id
    assert result["send_result"] is not None
    assert isinstance(result["events"], list)


def test_sessions_archive_and_delete(client: anthropic.Anthropic, tmp_agent, tmp_env):
    """Create → archive (assert) → delete, all on one session."""
    result = SessionExamples.archive_and_delete(client, tmp_agent.id, tmp_env)
    assert result["archived"].id == result["session"].id


def test_sessions_update(client: anthropic.Anthropic, tmp_agent, tmp_env):
    result = SessionExamples.update_session(client, tmp_agent.id, tmp_env)
    assert result["updated"].title == "sdk-e2e-update-after"


def test_sessions_resources_crud(
    client: anthropic.Anthropic, tmp_agent, tmp_env, tmp_file,
):
    result = SessionExamples.resource_crud(
        client, tmp_agent.id, tmp_env, tmp_file,
    )
    assert result["resource"].id


def test_sessions_threads(client: anthropic.Anthropic, tmp_agent, tmp_env):
    result = SessionExamples.thread_retrieve_and_archive(
        client, tmp_agent.id, tmp_env,
    )
    assert result["archived"].status == "archived"


def test_sessions_vault_ids(
    client: anthropic.Anthropic, tmp_agent, tmp_env,
):
    created = VaultExamples.create_and_retrieve(
        client, name="sdk-session-vault",
    )
    vault_id = created["vault"].id
    result = SessionExamples.vault_ids_round_trip(
        client, tmp_agent.id, tmp_env, vault_id,
    )
    assert result["retrieved"].vault_ids == [vault_id]
    client.beta.vaults.archive(vault_id)
