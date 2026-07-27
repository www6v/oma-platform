"""Tests for assembling assistant text from streamed OMA wire events."""

from oma_adapter.assistant_text import assemble_assistant_text


def _msg(text: str) -> dict:
    return {"type": "agent.message", "content": [{"type": "text", "text": text}]}


def test_assemble_joins_streaming_deltas():
    events = [
        _msg("Titles:\n"),
        _msg("1. Attention Is All You Need\n"),
        _msg("2. BERT\n"),
        _msg("25"),
    ]
    assert assemble_assistant_text(events) == (
        "Titles:\n1. Attention Is All You Need\n2. BERT\n25"
    )


def test_assemble_last_segment_after_tool_round():
    events = [
        _msg("I will search arxiv."),
        {"type": "agent.tool_use", "name": "web_search", "id": "t1"},
        {
            "type": "agent.tool_result",
            "tool_use_id": "t1",
            "content": [{"type": "text", "text": "raw hits"}],
        },
        _msg('["Paper A", "Paper B"]'),
    ]
    assert assemble_assistant_text(events) == '["Paper A", "Paper B"]'


def test_assemble_empty():
    assert assemble_assistant_text([]) == ""
