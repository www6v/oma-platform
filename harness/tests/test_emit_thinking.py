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
                    "content": [
                        {
                            "type": "thinking",
                            "thinking": "step",
                            "thinking_signature": "sig",
                        }
                    ],
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
        "agent.thinking",
    ]
    assert stream[1]["delta"] == "step"
    assert stream[0]["thinking_id"] == "think-0"
    thinking = stream[3]
    assert thinking["text"] == "step"
    assert thinking["thinking_id"] == "think-0"
    assert thinking["providerOptions"]["anthropic"]["signature"] == "sig"

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
    # Canonical thinking already emitted at thinking_end — do not repeat.
    assert "agent.thinking" not in types
    assert "agent.message" in types
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


def test_thinking_before_streamed_message_and_no_turn_end_dup() -> None:
    """Regression: streamed answer then message_end+turn_end must not
    place thinking after the message or emit it twice.
    """
    streaming_state: dict = {
        "last_emitted_len": 0,
        "live_thinking": set(),
        "emitted_thinking_ids": set(),
    }
    seen: set[str] = set()
    out: list[dict] = []

    def feed(raw: list[dict]) -> None:
        out.extend(
            emit_oma_events(
                raw,
                seen_agent_text=seen,
                streaming_state=streaming_state,
            )
        )

    feed([{"type": "message_start", "message": {"role": "assistant", "content": []}}])
    feed(
        [
            {
                "type": "message_update",
                "message": {
                    "role": "assistant",
                    "content": [
                        {
                            "type": "thinking",
                            "thinking": "Gauss formula",
                            "thinking_signature": "sig",
                        }
                    ],
                },
                "assistantMessageEvent": {
                    "type": "thinking_end",
                    "content_index": 0,
                    "content": "Gauss formula",
                },
            },
        ]
    )
    feed(
        [
            {
                "type": "message_update",
                "message": {
                    "role": "assistant",
                    "content": [
                        {
                            "type": "thinking",
                            "thinking": "Gauss formula",
                            "thinking_signature": "sig",
                        },
                        {"type": "text", "text": "5050"},
                    ],
                },
                "assistantMessageEvent": {
                    "type": "text_delta",
                    "delta": "5050",
                    "content_index": 1,
                },
            },
        ]
    )
    assistant = {
        "role": "assistant",
        "content": [
            {
                "type": "thinking",
                "thinking": "Gauss formula",
                "thinking_signature": "sig",
            },
            {"type": "text", "text": "5050"},
        ],
    }
    feed([{"type": "message_end", "message": assistant}])
    feed([{"type": "turn_end", "message": assistant}])

    types = [e["type"] for e in out]
    thinking_events = [e for e in out if e["type"] == "agent.thinking"]
    assert len(thinking_events) == 1
    assert thinking_events[0]["text"] == "Gauss formula"
    assert types.index("agent.thinking") < types.index("agent.message")
