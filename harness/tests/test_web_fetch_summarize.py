from __future__ import annotations

from typing import Any

import pytest
from pi_ai.types import TextContent

from oma_adapter.types import ModelConfig
from oma_adapter.web_fetch.runtime import WebFetchRuntime
from oma_adapter.web_fetch.summarize import summarize_page_markdown


@pytest.mark.asyncio
async def test_summarize_emits_aux_event() -> None:
    emitted: list[dict[str, Any]] = []

    async def emit(event: dict[str, Any]) -> None:
        emitted.append(event)

    class FakeUsage:
        input = 12
        output = 34

    class FakeMessage:
        content = [TextContent(text="digest")]
        usage = FakeUsage()

    async def fake_complete(*args: Any, **kwargs: Any) -> FakeMessage:
        del args, kwargs
        return FakeMessage()

    runtime = WebFetchRuntime(
        workdir="/tmp",
        emit_event=emit,
    )
    monkeypatch = pytest.MonkeyPatch()
    monkeypatch.setattr(
        "oma_adapter.web_fetch.summarize.complete_simple",
        fake_complete,
    )
    monkeypatch.setattr(
        "oma_adapter.web_fetch.summarize.resolve_pi_model",
        lambda _cfg: object(),
    )
    try:
        summary, usage = await summarize_page_markdown(
            url="https://example.com",
            markdown="x" * 100,
            aux_cfg=ModelConfig(model="test-model", provider="faux"),
            runtime=runtime,
        )
        assert summary == "digest"
        assert usage["input"] == 12
        assert len(emitted) == 1
        assert emitted[0]["type"] == "aux.model_call"
        assert emitted[0]["status"] == "ok"
    finally:
        monkeypatch.undo()
