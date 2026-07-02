"""Tests for custom-tool wire classification and pending detection (GT2)."""

from __future__ import annotations

from oma_adapter.custom_tools import (
    MAX_PENDING_EVENT_IDS,
    custom_tool_names,
    custom_tools_from_agent,
    is_wire_builtin_tool,
    pending_custom_tool_ids,
    wire_tool_use_type,
)
from oma_adapter.emit import emit_oma_events
from oma_adapter.types import AgentSnapshot


def test_custom_tools_from_agent() -> None:
    agent = AgentSnapshot(
        id="a1",
        name="gate",
        model="m",
        tools=[
            {"type": "agent_toolset_20260401"},
            {
                "type": "custom",
                "name": "decide",
                "description": "approve/reject",
                "input_schema": {"type": "object"},
            },
            {
                "type": "custom",
                "name": "escalate",
                "description": "human review",
                "input_schema": {},
            },
        ],
    )
    defs = custom_tools_from_agent(agent)
    assert [d.name for d in defs] == ["decide", "escalate"]
    assert custom_tool_names(agent) == frozenset({"decide", "escalate"})


def test_wire_tool_use_type_builtin_vs_custom() -> None:
    assert wire_tool_use_type("bash") == "agent.tool_use"
    assert wire_tool_use_type("decide") == "agent.custom_tool_use"
    assert is_wire_builtin_tool("mcp__github__search")


def test_emit_oma_events_custom_tool_use() -> None:
    raw = [
        {
            "type": "tool_use",
            "toolCallId": "ctu_1",
            "toolName": "decide",
            "args": {"receipt_id": "r01", "action": "approve", "reason": "ok"},
        }
    ]
    events = emit_oma_events(
        raw,
        custom_tool_names=frozenset({"decide"}),
    )
    assert len(events) == 1
    assert events[0]["type"] == "agent.custom_tool_use"
    assert events[0]["id"] == "ctu_1"


def test_emit_oma_events_bash_stays_tool_use() -> None:
    raw = [
        {
            "type": "tool_use",
            "toolCallId": "tool_1",
            "toolName": "bash",
            "args": {"command": "ls"},
        }
    ]
    events = emit_oma_events(raw)
    assert events[0]["type"] == "agent.tool_use"


def test_pending_custom_tool_ids_without_result() -> None:
    events = [
        {
            "type": "agent.custom_tool_use",
            "id": "ctu_a",
            "name": "decide",
            "input": {},
        },
        {
            "type": "agent.custom_tool_use",
            "id": "ctu_b",
            "name": "escalate",
            "input": {},
        },
        {
            "type": "agent.tool_result",
            "tool_use_id": "ctu_a",
            "content": [{"type": "text", "text": "{}"}],
        },
    ]
    assert pending_custom_tool_ids(events) == ["ctu_b"]


def test_pending_custom_tool_ids_sliding_window() -> None:
    events = [
        {
            "type": "agent.custom_tool_use",
            "id": f"ctu_{idx}",
            "name": "decide",
            "input": {},
        }
        for idx in range(7)
    ]
    pending = pending_custom_tool_ids(events)
    assert len(pending) == MAX_PENDING_EVENT_IDS
    assert pending[0] == "ctu_0"


def test_emit_suppresses_custom_tool_result_incremental() -> None:
    """Streaming listener emits start/end in separate deltas (GT2 regression)."""
    buffer = [
        {
            "type": "tool_execution_start",
            "toolCallId": "tc_decide",
            "toolName": "decide",
            "args": {"receipt_id": "r01", "action": "approve", "reason": "ok"},
        },
        {
            "type": "tool_execution_end",
            "toolCallId": "tc_decide",
            "result": {
                "content": [{"type": "text", "text": "should not emit"}],
                "is_error": False,
            },
        },
    ]
    first = emit_oma_events(
        buffer[:1],
        custom_tool_names=frozenset({"decide"}),
        event_lookup_buffer=buffer,
    )
    second = emit_oma_events(
        buffer[1:],
        custom_tool_names=frozenset({"decide"}),
        event_lookup_buffer=buffer,
    )
    events = [*first, *second]
    assert [ev["type"] for ev in events] == ["agent.custom_tool_use"]
    assert pending_custom_tool_ids(events) == ["tc_decide"]


def test_emit_suppresses_custom_tool_result() -> None:
    raw = [
        {
            "type": "tool_execution_start",
            "toolCallId": "tc_decide",
            "toolName": "decide",
            "args": {"receipt_id": "r01", "action": "approve", "reason": "ok"},
        },
        {
            "type": "tool_execution_end",
            "toolCallId": "tc_decide",
            "result": {
                "content": [{"type": "text", "text": "should not emit"}],
                "is_error": False,
            },
        },
    ]
    events = emit_oma_events(
        raw,
        custom_tool_names=frozenset({"decide"}),
    )
    types = [ev["type"] for ev in events]
    assert types == ["agent.custom_tool_use"]
    assert pending_custom_tool_ids(events) == ["tc_decide"]


def test_register_custom_tools_on_session(monkeypatch) -> None:
    from oma_adapter.custom_tools import (
        CustomToolDef,
        register_custom_tools_on_session,
    )

    class StubTool:
        def __init__(self, name: str) -> None:
            self.name = name

    def fake_stub(defn: CustomToolDef) -> StubTool:
        return StubTool(defn.name)

    monkeypatch.setattr(
        "oma_adapter.custom_tools.make_custom_tool_stub",
        fake_stub,
    )

    class FakeAgent:
        def __init__(self) -> None:
            self._tools = [StubTool("bash")]

    class FakeSession:
        def __init__(self) -> None:
            self._agent = FakeAgent()

    session = FakeSession()
    register_custom_tools_on_session(
        session,
        [
            CustomToolDef(
                name="decide",
                description="approve/reject",
                input_schema={"type": "object"},
            ),
            CustomToolDef(
                name="escalate",
                description="human review",
                input_schema={},
            ),
        ],
    )
    names = {tool.name for tool in session._agent._tools}
    assert names == {"bash", "decide", "escalate"}
