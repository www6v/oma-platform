from oma_adapter.emit import emit_oma_events


def test_emit_call_agent_thread_message_events() -> None:
    events = emit_oma_events([
        {
            "type": "tool_use",
            "id": "call_delegate_1",
            "name": "call_agent_agent_researcher",
            "args": {"message": "research the prospect"},
        },
        {
            "type": "tool_result",
            "toolCallId": "call_delegate_1",
            "result": {"content": [{"type": "text", "text": '{"ok": true}'}]},
        },
    ])
    types = [item["type"] for item in events]
    assert types == [
        "agent.tool_use",
        "agent.thread_message_sent",
        "agent.tool_result",
        "agent.thread_message_received",
    ]
    received = events[-1]
    assert received["from_agent_id"] == "agent_researcher"
    assert received["parent_event_id"] == "sevt-thread-sent-call_delegate_1"
