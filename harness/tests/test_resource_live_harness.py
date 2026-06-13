"""Real piPy harness: resources mount inside run_turn_stream before agent runs."""

from __future__ import annotations

import asyncio
import base64
import tempfile
from pathlib import Path
from typing import Any

import pytest

pytest.importorskip("pi_coding_agent")

from pi_ai.providers.faux import (
    faux_assistant_message,
    faux_text,
    faux_tool_call,
    register_faux_provider,
    set_faux_responses,
)

from oma_adapter.turn import run_turn_stream
from oma_adapter.types import AgentSnapshot

FAUX_MODEL = "faux/resource-live"
MOUNTED_FILE_CONTENT = "resource-live-ok"
MOUNTED_ENV = "RESOURCE_LIVE"
MOUNTED_ENV_VALUE = "yes"
FINAL_REPLY = "mounted resources verified"


def _message_text(event: dict[str, Any]) -> str:
    parts: list[str] = []
    for block in event.get("content") or []:
        if not isinstance(block, dict):
            continue
        if block.get("type") != "text":
            continue
        text = block.get("text")
        if isinstance(text, str):
            parts.append(text)
    return " ".join(parts)


def test_resource_mounter_real_harness_turn_reads_mounts() -> None:
    """pi session turn mounts file/env, then bash reads them during the turn."""
    registration = register_faux_provider(
        models=[{"id": "resource-live", "name": "resource-live"}],
    )
    set_faux_responses(
        [
            faux_assistant_message(
                [
                    faux_tool_call(
                        "bash",
                        {
                            "command": (
                                "cat mnt/session/uploads/mounted.txt"
                                " && printenv RESOURCE_LIVE"
                            ),
                        },
                    )
                ]
            ),
            faux_assistant_message(
                [
                    faux_text(
                        f"{FINAL_REPLY}: {MOUNTED_FILE_CONTENT} "
                        f"{MOUNTED_ENV_VALUE}"
                    )
                ]
            ),
        ]
    )

    emitted: list[dict[str, Any]] = []

    async def on_event(event: dict[str, Any]) -> None:
        emitted.append(event)

    async def run() -> None:
        agent = AgentSnapshot(
            id="agt_resource_live",
            name="ResourceLive",
            model=FAUX_MODEL,
            system_prompt="Use bash to inspect mounted session resources.",
        )
        resources = [
            {
                "type": "file",
                "mount_path": "/mnt/session/uploads/mounted.txt",
                "content_base64": base64.b64encode(
                    MOUNTED_FILE_CONTENT.encode("utf-8")
                ).decode("ascii"),
            },
            {
                "type": "env",
                "name": MOUNTED_ENV,
                "value": MOUNTED_ENV_VALUE,
            },
        ]
        with tempfile.TemporaryDirectory() as workdir:
            mounted_path = Path(workdir) / "mnt/session/uploads/mounted.txt"
            assert not mounted_path.exists()

            resp = await run_turn_stream(
                session_id="sess_resource_live",
                agent=agent,
                resources=resources,
                events=[
                    {
                        "type": "user.message",
                        "content": [
                            {
                                "type": "text",
                                "text": "Read the mounted file and env var.",
                            }
                        ],
                    }
                ],
                workdir=workdir,
                on_event=on_event,
            )
            assert resp.events
            assert mounted_path.read_text() == MOUNTED_FILE_CONTENT

    try:
        asyncio.run(run())
    finally:
        registration.dispose()

    bash_uses = [
        ev
        for ev in emitted
        if ev.get("type") == "agent.tool_use" and ev.get("name") == "bash"
    ]
    assert bash_uses, "expected bash tool_use from live harness turn"

    agent_msgs = [ev for ev in emitted if ev.get("type") == "agent.message"]
    assert any(MOUNTED_FILE_CONTENT in _message_text(ev) for ev in agent_msgs)
