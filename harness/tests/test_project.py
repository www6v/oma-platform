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
