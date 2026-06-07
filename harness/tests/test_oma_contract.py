from oma_adapter.emit import emit_oma_events


def test_user_message_shape() -> None:
    event = {
        "type": "user.message",
        "content": [{"type": "text", "text": "Run: uname -a"}],
    }
    assert event["type"] == "user.message"


def test_agent_message_roundtrip() -> None:
    raw = [{"type": "assistant_message", "content": [{"type": "text", "text": "done"}]}]
    out = emit_oma_events(raw)
    assert out[0]["type"] == "agent.message"
    assert out[0]["content"][0]["type"] == "text"


def test_turn_end_message_roundtrip() -> None:
    raw = [
        {
            "type": "turn_end",
            "message": {
                "role": "assistant",
                "content": [{"type": "text", "text": "hello from model"}],
            },
        }
    ]
    out = emit_oma_events(raw)
    assert out[0]["content"][0]["text"] == "hello from model"
