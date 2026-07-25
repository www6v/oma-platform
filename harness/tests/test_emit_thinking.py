"""Thinking stream + canonical agent.thinking mapping from pi events."""

from oma_adapter.emit import emit_oma_events


def test_emit_thinking_stream_and_canonical() -> None:
    streaming_state: dict = {
        "last_emitted_len": 0,
        "live_thinking": set(),
        "emitted_thinking_ids": set(),
    }
    stream = emit_oma_events(
        [
            {"type": "message_start", "message": {"role": "assistant", "content": []}},
            {
                "type": "message_update",
                "message": {
                    "role": "assistant",
                    "content": [{"type": "thinking", "thinking": ""}],
                },
                "assistantMessageEvent": {
                    "type": "thinking_start",
                    "content_index": 0,
                },
            },
            {
                "type": "message_update",
                "message": {
                    "role": "assistant",
                    "content": [{"type": "thinking", "thinking": "step"}],
                },
                "assistantMessageEvent": {
                    "type": "thinking_delta",
                    "content_index": 0,
                    "delta": "step",
                },
            },
            {
                "type": "message_update",
                "message": {
                    "role": "assistant",
                    "content": [{"type": "thinking", "thinking": "step"}],
                },
                "assistantMessageEvent": {
                    "type": "thinking_end",
                    "content_index": 0,
                    "content": "step",
                },
            },
        ],
        streaming_state=streaming_state,
    )
    assert [e["type"] for e in stream] == [
        "agent.thinking_stream_start",
        "agent.thinking_chunk",
        "agent.thinking_stream_end",
    ]
    assert stream[1]["delta"] == "step"
    assert stream[0]["thinking_id"] == "think-0"

    final = emit_oma_events(
        [
            {
                "type": "message_end",
                "message": {
                    "role": "assistant",
                    "content": [
                        {
                            "type": "thinking",
                            "thinking": "step",
                            "thinking_signature": "sig",
                        },
                        {"type": "text", "text": "hello"},
                    ],
                },
            },
        ],
        streaming_state=streaming_state,
    )
    types = [e["type"] for e in final]
    assert "agent.thinking" in types
    assert "agent.message" in types
    thinking = next(e for e in final if e["type"] == "agent.thinking")
    assert thinking["text"] == "step"
    assert thinking["thinking_id"] == "think-0"
    assert thinking["providerOptions"]["anthropic"]["signature"] == "sig"
    message = next(e for e in final if e["type"] == "agent.message")
    assert message["content"][0]["text"] == "hello"


def test_emit_thinking_without_stream() -> None:
    events = emit_oma_events(
        [
            {
                "type": "message_end",
                "message": {
                    "role": "assistant",
                    "content": [
                        {"type": "thinking", "thinking": "plan"},
                        {"type": "text", "text": "ok"},
                    ],
                },
            },
        ]
    )
    types = [e["type"] for e in events]
    assert types.index("agent.thinking") < types.index("agent.message")
    thinking = next(e for e in events if e["type"] == "agent.thinking")
    assert thinking["text"] == "plan"
    assert thinking["thinking_id"] == "think-0"


def test_thinking_text_not_merged_into_agent_message() -> None:
    events = emit_oma_events(
        [
            {
                "type": "message_update",
                "message": {
                    "role": "assistant",
                    "content": [
                        {"type": "thinking", "thinking": "secret"},
                        {"type": "text", "text": "visible"},
                    ],
                },
                "assistantMessageEvent": {
                    "type": "text_delta",
                    "delta": "visible",
                    "content_index": 1,
                },
            },
        ]
    )
    assert len(events) == 1
    assert events[0]["type"] == "agent.message"
    assert events[0]["content"][0]["text"] == "visible"
