from oma_adapter.project import (
    PRIMARY_THREAD_ID,
    latest_user_text,
    project_oma_events,
)


def test_project_oma_events_scoped_to_thread() -> None:
    events = [
        {
            "type": "user.message",
            "content": [{"type": "text", "text": "leader task"}],
        },
        {
            "type": "user.message",
            "session_thread_id": "sthr_coder",
            "content": [{"type": "text", "text": "write quicksort"}],
        },
    ]
    assert project_oma_events(events) == "leader task"
    assert (
        project_oma_events(events, session_thread_id="sthr_coder")
        == "write quicksort"
    )
    assert latest_user_text(events) == "leader task"
    assert (
        latest_user_text(events, session_thread_id="sthr_coder")
        == "write quicksort"
    )
    assert PRIMARY_THREAD_ID == "sthr_primary"
