from oma_adapter.project import latest_user_text, project_oma_events


def test_latest_user_text() -> None:
    events = [
        {"type": "user.message", "content": [{"type": "text", "text": "hello"}]},
    ]
    assert project_oma_events(events) == "hello"
    assert latest_user_text(events) == "hello"


def test_project_oma_events_includes_prior_turns() -> None:
    events = [
        {
            "type": "user.message",
            "content": [{"type": "text", "text": "My name is Alice."}],
        },
        {
            "type": "agent.message",
            "content": [{"type": "text", "text": "Nice to meet you, Alice."}],
        },
        {
            "type": "user.message",
            "content": [{"type": "text", "text": "What is my name?"}],
        },
    ]
    prompt = project_oma_events(events)
    assert "Alice" in prompt
    assert "What is my name?" in prompt
    assert "<conversation-history>" in prompt


def test_project_oma_events_hitl_continuation_after_user_message() -> None:
    """Phase D: tool results after the triggering user.message appear in history."""
    events = [
        {
            "type": "user.message",
            "content": [{"type": "text", "text": "Process receipts."}],
        },
        {
            "type": "agent.custom_tool_use",
            "id": "ctu_1",
            "name": "decide",
            "input": {"receipt_id": "r01", "action": "approve"},
        },
        {
            "type": "user.custom_tool_result",
            "custom_tool_use_id": "ctu_1",
            "content": [{"type": "text", "text": '{"ok": true}'}],
        },
        {
            "type": "agent.tool_result",
            "tool_use_id": "ctu_1",
            "content": [{"type": "text", "text": '{"ok": true}'}],
        },
    ]
    prompt = project_oma_events(events)
    assert "Process receipts." in prompt
    assert "decide" in prompt
    assert "ctu_1" in prompt
    assert "<conversation-history>" in prompt
