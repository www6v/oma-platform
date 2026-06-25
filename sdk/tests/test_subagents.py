"""E2E tests for sub-agent configuration and session thread APIs."""

from __future__ import annotations

import os

import anthropic
import httpx

from oma_sdk.examples import SubagentExamples
from oma_sdk.subagent import build_general_subagent_metadata

_KEEP = os.getenv("OMA_KEEP_RESOURCES", "0") == "1"


def test_subagent_coordinator_multiagent(client: anthropic.Anthropic):
    """Worker + coordinator: multiagent roster round-trips via retrieve."""
    result = SubagentExamples.create_coordinator_and_verify(client)
    assert result["worker"].id
    assert result["coordinator"].id
    assert result["worker"].id in result["roster_ids"]


def test_subagent_general_subagent_metadata(client: anthropic.Anthropic):
    """enable_general_subagent persists in agent metadata."""
    agent = SubagentExamples.create_with_general_subagent(client)
    try:
        assert agent["id"]
        resp = client.get(
            f"/v1/agents/{agent['id']}",
            cast_to=httpx.Response,
        )
        metadata = resp.json().get("metadata") or {}
        assert metadata.get("enable_general_subagent") is True
        assert metadata == build_general_subagent_metadata()
    finally:
        if not _KEEP:
            try:
                client.beta.agents.archive(agent["id"])
            except Exception:
                pass


def test_subagent_role_defaults_metadata(
    client: anthropic.Anthropic,
    tmp_agent,
):
    """default_subagent_roles metadata round-trips on create."""
    roles = {"explore": tmp_agent.id}
    agent = SubagentExamples.create_with_role_defaults(client, roles)
    try:
        assert agent["id"]
        resp = client.get(
            f"/v1/agents/{agent['id']}",
            cast_to=httpx.Response,
        )
        metadata = resp.json().get("metadata") or {}
        assert metadata.get("default_subagent_roles") == roles
    finally:
        if not _KEEP:
            try:
                client.beta.agents.archive(agent["id"])
            except Exception:
                pass


def test_subagent_setup_coordinator_session(
    client: anthropic.Anthropic,
    tmp_env,
):
    """Eval-style setup: subAgents → coordinator → session."""
    setup = SubagentExamples.setup_coordinator_session(
        client,
        tmp_env,
    )
    try:
        assert len(setup["workers"]) == 1
        assert setup["coordinator"].multiagent is not None
        assert setup["session"].id

        verified = SubagentExamples.verify_coordinator_multiagent(
            client,
            setup["coordinator"].id,
            [setup["workers"][0].id],
        )
        assert verified["roster_ids"]
    finally:
        if not _KEEP:
            client.beta.sessions.archive(setup["session"].id)
            client.beta.agents.archive(setup["coordinator"].id)
            for worker in setup["workers"]:
                try:
                    client.beta.agents.archive(worker.id)
                except Exception:
                    pass


def test_subagent_session_threads_and_trajectory(
    client: anthropic.Anthropic,
    tmp_agent,
    tmp_env,
):
    """Threads and trajectory endpoints return primary thread for new session."""
    result = SubagentExamples.session_threads_and_trajectory(
        client,
        tmp_agent.id,
        tmp_env,
    )
    assert result["threads"][0]["id"] == "sthr_primary"
    assert result["trajectory"]["summary"]["num_threads"] >= 1
