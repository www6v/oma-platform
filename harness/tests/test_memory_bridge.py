"""Tests for the memory bridge (OMA host side of pi_memory)."""

from __future__ import annotations

import asyncio
from pathlib import Path

import pytest

from oma_adapter import memory_bridge
from oma_adapter.tools import (
    _extension_paths_for_agent,
    resolve_memory_extension_path,
)
from oma_adapter.types import AgentSnapshot


# ---------------------------------------------------------------- memory_enabled


def test_memory_disabled_by_default(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("OMA_MEMORY_ENABLED", raising=False)
    assert memory_bridge.memory_enabled() is False


def test_memory_enabled_with_flag(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("OMA_MEMORY_ENABLED", "1")
    assert memory_bridge.memory_enabled() is True


def test_memory_enabled_requires_exact_one(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("OMA_MEMORY_ENABLED", "true")
    assert memory_bridge.memory_enabled() is False


# ---------------------------------------------------------- user text extraction


def test_extract_last_user_text_string_content() -> None:
    events = [
        {"type": "user.message", "content": "first"},
        {"type": "agent.message", "content": [{"type": "text", "text": "hi"}]},
        {"type": "user.message", "content": "second"},
    ]
    assert memory_bridge._extract_last_user_text(events) == "second"


def test_extract_last_user_text_part_content() -> None:
    events = [
        {
            "type": "user.message",
            "content": [
                {"type": "text", "text": "hello"},
                {"type": "image", "url": "http://x"},
                {"type": "text", "text": "world"},
            ],
        }
    ]
    assert memory_bridge._extract_last_user_text(events) == "hello\nworld"


def test_extract_last_user_text_empty() -> None:
    assert memory_bridge._extract_last_user_text(None) == ""
    assert memory_bridge._extract_last_user_text([]) == ""
    assert (
        memory_bridge._extract_last_user_text(
            [{"type": "agent.message", "content": "x"}, "junk"]
        )
        == ""
    )


# ------------------------------------------------------------ build_memory_runtime


def test_build_runtime_disabled(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.delenv("OMA_MEMORY_ENABLED", raising=False)
    assert (
        memory_bridge.build_memory_runtime(
            session_id="s",
            tenant_id="t",
            agent_id="a",
            workdir="/tmp",
            platform_base="http://platform",
            internal_secret="secret",
            events=None,
        )
        is None
    )


def test_build_runtime_missing_platform_base(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("OMA_MEMORY_ENABLED", "1")
    monkeypatch.delenv("OMA_INTERNAL_SECRET", raising=False)
    assert (
        memory_bridge.build_memory_runtime(
            session_id="s",
            tenant_id="t",
            agent_id="a",
            workdir="/tmp",
            platform_base=None,
            internal_secret="secret",
            events=None,
        )
        is None
    )


def test_build_runtime_missing_agent_or_workdir(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("OMA_MEMORY_ENABLED", "1")
    for agent_id, workdir in [("", "/tmp"), ("a", "")]:
        assert (
            memory_bridge.build_memory_runtime(
                session_id="s",
                tenant_id="t",
                agent_id=agent_id,
                workdir=workdir,
                platform_base="http://platform",
                internal_secret="secret",
                events=None,
            )
            is None
        )


def test_build_runtime_happy_path(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("OMA_MEMORY_ENABLED", "1")
    monkeypatch.delenv("OMA_INTERNAL_SECRET", raising=False)
    runtime = memory_bridge.build_memory_runtime(
        session_id="sess-1",
        tenant_id=None,
        agent_id="agent-1",
        workdir="/tmp/wd",
        platform_base="http://platform",
        internal_secret="secret",
        events=[{"type": "user.message", "content": "记住我喜欢咖啡"}],
    )
    assert runtime is not None
    assert runtime.session_id == "sess-1"
    assert runtime.tenant_id == "default"  # fallback tenant
    assert runtime.agent_id == "agent-1"
    assert runtime.workdir == "/tmp/wd"
    assert runtime.platform_base == "http://platform"
    assert runtime.internal_secret == "secret"
    assert runtime.last_user_text == "记住我喜欢咖啡"
    assert len(runtime.turn_uuid) == 8


def test_build_runtime_internal_secret_env_fallback(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("OMA_MEMORY_ENABLED", "1")
    monkeypatch.setenv("OMA_INTERNAL_SECRET", "env-secret")
    runtime = memory_bridge.build_memory_runtime(
        session_id="s",
        tenant_id="t",
        agent_id="a",
        workdir="/tmp",
        platform_base="http://platform",
        internal_secret=None,
        events=None,
    )
    assert runtime is not None
    assert runtime.internal_secret == "env-secret"


# ------------------------------------------------------- configure/reset runtime


def test_configure_and_reset_runtime(monkeypatch: pytest.MonkeyPatch) -> None:
    monkeypatch.setenv("OMA_MEMORY_ENABLED", "1")
    from pi_memory.runtime import get_memory_runtime

    runtime = memory_bridge.build_memory_runtime(
        session_id="s",
        tenant_id="t",
        agent_id="a",
        workdir="/tmp",
        platform_base="http://platform",
        internal_secret="secret",
        events=None,
    )
    token = memory_bridge.configure_runtime(runtime)
    assert get_memory_runtime() is runtime
    memory_bridge.reset_runtime(token)
    assert get_memory_runtime() is None


def test_configure_runtime_none_is_noop() -> None:
    assert memory_bridge.configure_runtime(None) is None
    memory_bridge.reset_runtime(None)  # must not raise


@pytest.mark.asyncio
async def test_drain_background_tasks() -> None:
    from pi_memory.runtime import MemoryRuntime

    done: list[str] = []

    async def work(label: str) -> None:
        await asyncio.sleep(0)
        done.append(label)

    runtime = MemoryRuntime(
        session_id="s",
        tenant_id="t",
        agent_id="a",
        workdir="/tmp",
        platform_base="http://platform",
        internal_secret="secret",
    )
    loop = asyncio.get_running_loop()
    runtime.background_tasks = [
        loop.create_task(work("one")),
        loop.create_task(work("two")),
    ]
    await memory_bridge.drain_memory_background_tasks(runtime)
    assert done == ["one", "two"]
    await memory_bridge.drain_memory_background_tasks(None)  # no-op


# ------------------------------------------------------------- extension path


def test_resolve_memory_extension_path_default() -> None:
    path = resolve_memory_extension_path()
    assert path.name == "memory_extension.py"
    assert path.is_file()
    assert "piPy-hermes-memory" in str(path)


def test_resolve_memory_extension_path_env_override(
    monkeypatch: pytest.MonkeyPatch,
    tmp_path: Path,
) -> None:
    custom = tmp_path / "custom_memory_extension.py"
    custom.write_text("# stub\n", encoding="utf-8")
    monkeypatch.setenv("PIPY_MEMORY_EXTENSION", str(custom))
    assert resolve_memory_extension_path() == custom


def test_extension_paths_include_memory_when_enabled(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.setenv("OMA_MEMORY_ENABLED", "1")
    agent = AgentSnapshot(id="a", name="n", model="m")
    paths = _extension_paths_for_agent(agent)
    assert any(p.endswith("memory_extension.py") for p in paths)


def test_extension_paths_exclude_memory_when_disabled(
    monkeypatch: pytest.MonkeyPatch,
) -> None:
    monkeypatch.delenv("OMA_MEMORY_ENABLED", raising=False)
    agent = AgentSnapshot(id="a", name="n", model="m")
    paths = _extension_paths_for_agent(agent)
    assert not any(p.endswith("memory_extension.py") for p in paths)
