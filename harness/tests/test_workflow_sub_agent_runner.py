"""Tests for ``OmaSubAgentRunner`` (workflow sub-agent backend)."""

from __future__ import annotations

import sys
from typing import Any
from unittest.mock import MagicMock, patch

import pytest

from pi_dynamic_workflows.lib.sub_agent_types import SubAgentOptions

from oma_adapter.workflow_sub_agent_runner import OmaSubAgentRunner


@pytest.mark.asyncio
async def test_oma_runner_emits_thread_lifecycle(monkeypatch):
    emitted: list[dict[str, Any]] = []

    async def fake_emit(event: dict[str, Any]) -> None:
        emitted.append(event)

    async def fake_sub_turn(**kwargs: Any) -> MagicMock:
        return MagicMock(
            events=[
                {
                    "type": "agent.message",
                    "content": [{"type": "text", "text": "done"}],
                },
            ],
        )

    worker = MagicMock(
        id="worker-1",
        name="workflow:smoke-worker-step1",
        model="faux/test",
    )
    parent = MagicMock(id="coord", name="Coord", model="faux/test")
    runtime = MagicMock(
        session_id="sess_test",
        tenant_id="default",
        workdir="/tmp",
        parent_agent=parent,
        parent_thread_id="sthr_primary",
        sub_agents={"worker-1": worker},
        model=None,
        aux_model=None,
        environment=None,
        mcp_proxy_base=None,
        mcp_proxy_api_key=None,
        outbound_proxy_addr=None,
        outbound_proxy_api_key=None,
        depth=0,
        emit_event=fake_emit,
        run_sub_turn=fake_sub_turn,
    )
    worker.model_copy.return_value = worker

    mock_events = MagicMock()
    mock_events.new_thread_id.return_value = "sthr_test_thread"

    mock_runtime_mod = MagicMock()
    mock_runtime_mod.get_subagent_runtime.return_value = runtime

    mock_types = MagicMock()
    mock_types.SubAgentSnapshot.return_value = worker

    mock_pi_subagent = MagicMock()
    mock_pi_subagent.events = mock_events
    mock_pi_subagent.runtime = mock_runtime_mod
    mock_pi_subagent.types = mock_types

    with patch.dict(
        sys.modules,
        {
            "pi_subagent": mock_pi_subagent,
            "pi_subagent.events": mock_events,
            "pi_subagent.runtime": mock_runtime_mod,
            "pi_subagent.types": mock_types,
        },
    ):
        monkeypatch.setattr(
            "oma_adapter.workflow_sub_agent_runner.get_workflow_worker_map",
            lambda: {"step1": "worker-1"},
        )

        runner = OmaSubAgentRunner()
        result = await runner.run(
            "do task",
            SubAgentOptions(label="step1", action="llm_execute"),
        )

    assert result.content == "done"
    types = [ev["type"] for ev in emitted]
    assert types[0] == "session.thread_created"
    assert "session.sub_agent_started" in types
    assert "agent.tool_use" in types
    assert "user.message" in types
    assert "agent.tool_result" in types
    assert "session.sub_agent_completed" in types
    assert types[-1] == "session.thread_idle"
    assert emitted[0]["agent_id"] == "worker-1"
    assert emitted[0]["session_thread_id"] == emitted[-1]["session_thread_id"]
    primary_tools = [
        ev for ev in emitted
        if ev.get("session_thread_id") == "sthr_primary"
        and ev.get("type") in {"agent.tool_use", "agent.tool_result"}
    ]
    assert len(primary_tools) == 2


@pytest.mark.asyncio
async def test_oma_runner_joins_streamed_message_deltas(monkeypatch):
    """Step output must be full reply, not last streaming chunk only."""
    emitted: list[dict[str, Any]] = []

    async def fake_emit(event: dict[str, Any]) -> None:
        emitted.append(event)

    async def fake_sub_turn(**kwargs: Any) -> MagicMock:
        return MagicMock(
            events=[
                {
                    "type": "agent.message",
                    "content": [{"type": "text", "text": "Titles:\n"}],
                },
                {
                    "type": "agent.message",
                    "content": [{"type": "text", "text": "1. Paper A\n"}],
                },
                {
                    "type": "agent.message",
                    "content": [{"type": "text", "text": "2. Paper B"}],
                },
            ],
        )

    worker = MagicMock(
        id="worker-1",
        name="workflow:smoke-worker-step1",
        model="faux/test",
    )
    parent = MagicMock(id="coord", name="Coord", model="faux/test")
    runtime = MagicMock(
        session_id="sess_test",
        tenant_id="default",
        workdir="/tmp",
        parent_agent=parent,
        parent_thread_id="sthr_primary",
        sub_agents={"worker-1": worker},
        model=None,
        aux_model=None,
        environment=None,
        mcp_proxy_base=None,
        mcp_proxy_api_key=None,
        outbound_proxy_addr=None,
        outbound_proxy_api_key=None,
        depth=0,
        emit_event=fake_emit,
        run_sub_turn=fake_sub_turn,
    )
    worker.model_copy.return_value = worker

    mock_events = MagicMock()
    mock_events.new_thread_id.return_value = "sthr_test_thread"
    mock_runtime_mod = MagicMock()
    mock_runtime_mod.get_subagent_runtime.return_value = runtime
    mock_types = MagicMock()
    mock_types.SubAgentSnapshot.return_value = worker
    mock_pi_subagent = MagicMock()
    mock_pi_subagent.events = mock_events
    mock_pi_subagent.runtime = mock_runtime_mod
    mock_pi_subagent.types = mock_types

    with patch.dict(
        sys.modules,
        {
            "pi_subagent": mock_pi_subagent,
            "pi_subagent.events": mock_events,
            "pi_subagent.runtime": mock_runtime_mod,
            "pi_subagent.types": mock_types,
        },
    ):
        monkeypatch.setattr(
            "oma_adapter.workflow_sub_agent_runner.get_workflow_worker_map",
            lambda: {"step1": "worker-1"},
        )
        result = await OmaSubAgentRunner().run(
            "extract titles",
            SubAgentOptions(label="step1", action="llm_analyze"),
        )

    assert result.content == "Titles:\n1. Paper A\n2. Paper B"
