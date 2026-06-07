from oma_adapter.project import latest_user_text, project_oma_events


def test_latest_user_text() -> None:
    events = [
        {"type": "user.message", "content": [{"type": "text", "text": "hello"}]},
    ]
    assert project_oma_events(events) == "hello"
    assert latest_user_text(events) == "hello"
