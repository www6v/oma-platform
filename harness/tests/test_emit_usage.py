from oma_adapter.emit import emit_oma_events


def test_emit_model_usage_span_from_message_end() -> None:
    events = emit_oma_events([
        {
            "type": "message_end",
            "message": {
                "role": "assistant",
                "content": [{"type": "text", "text": "hello"}],
                "usage": {
                    "input": 11,
                    "output": 3,
                },
            },
        },
    ])
    types = [item["type"] for item in events]
    assert "agent.message" in types
    assert "span.model_request_end" in types
    span = next(item for item in events if item["type"] == "span.model_request_end")
    assert span["model_usage"]["input_tokens"] == 11
    assert span["model_usage"]["output_tokens"] == 3
