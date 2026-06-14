"""Tests for schedule wakeup HTTP client."""

from __future__ import annotations

import asyncio
from typing import Any

from oma_adapter.schedule.client import (
    cancel_wakeup,
    list_wakeups,
    schedule_wakeup,
)
from oma_adapter.schedule.runtime import ScheduleRuntime, configure_schedule


def _configure() -> None:
    configure_schedule(
        ScheduleRuntime(
            session_id="sess-test",
            platform_base="http://127.0.0.1:8787",
            internal_secret="secret",
        ),
    )


class FakeResponse:
    def __init__(
        self,
        status_code: int,
        payload: dict[str, Any],
        text: str = "",
    ) -> None:
        self.status_code = status_code
        self._payload = payload
        self.text = text

    def json(self) -> dict[str, Any]:
        return self._payload


class FakeClient:
    def __init__(self, *args: Any, **kwargs: Any) -> None:
        del args, kwargs
        self.post_calls: list[tuple[str, dict[str, Any]]] = []
        self.delete_calls: list[str] = []
        self.get_calls: list[str] = []

    async def __aenter__(self) -> FakeClient:
        return self

    async def __aexit__(self, *args: Any) -> None:
        del args

    async def post(self, url: str, **kwargs: Any) -> FakeResponse:
        self.post_calls.append((url, kwargs))
        return FakeResponse(
            201,
            {"id": "sched-abc", "kind": "one_shot"},
        )

    async def delete(self, url: str, **kwargs: Any) -> FakeResponse:
        del kwargs
        self.delete_calls.append(url)
        return FakeResponse(200, {"cancelled": True})

    async def get(self, url: str, **kwargs: Any) -> FakeResponse:
        del kwargs
        self.get_calls.append(url)
        return FakeResponse(200, {"schedules": [{"id": "sched-abc"}]})


def test_schedule_wakeup_posts(monkeypatch: Any) -> None:
    fake = FakeClient()

    def factory(*args: Any, **kwargs: Any) -> FakeClient:
        del args, kwargs
        return fake

    monkeypatch.setattr(
        "oma_adapter.schedule.client.httpx.AsyncClient",
        factory,
    )

    async def run() -> None:
        _configure()
        data = await schedule_wakeup(
            {"delay_seconds": 60, "prompt": "check build"},
        )
        assert data["id"] == "sched-abc"
        assert len(fake.post_calls) == 1
        assert fake.post_calls[0][1]["json"]["prompt"] == "check build"

    asyncio.run(run())


def test_cancel_wakeup_deletes(monkeypatch: Any) -> None:
    fake = FakeClient()

    def factory(*args: Any, **kwargs: Any) -> FakeClient:
        del args, kwargs
        return fake

    monkeypatch.setattr(
        "oma_adapter.schedule.client.httpx.AsyncClient",
        factory,
    )

    async def run() -> None:
        _configure()
        data = await cancel_wakeup("sched-abc")
        assert data["cancelled"] is True
        assert fake.delete_calls

    asyncio.run(run())


def test_list_wakeups_gets(monkeypatch: Any) -> None:
    fake = FakeClient()

    def factory(*args: Any, **kwargs: Any) -> FakeClient:
        del args, kwargs
        return fake

    monkeypatch.setattr(
        "oma_adapter.schedule.client.httpx.AsyncClient",
        factory,
    )

    async def run() -> None:
        _configure()
        data = await list_wakeups()
        assert len(data["schedules"]) == 1

    asyncio.run(run())
