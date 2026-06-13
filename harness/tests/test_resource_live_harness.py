"""Resource mounter live tests.

- ``test_resource_mounter_faux_harness_turn``: in-process faux piPy turn (CI).
- ``test_resource_mounter_platform_live_harness``: real oma-server session +
  real harness sidecar; creates persistent agent + session visible in Console.

Platform live test prerequisites (same as ``scripts/smoke-resource-live-e2e.sh``):

  - oma-server on ``OMA_LISTEN_ADDR`` (default ``:8787``) with ``OMA_FAKE_HARNESS=0``
  - harness sidecar on ``HARNESS_URL`` (default ``http://127.0.0.1:8090``)
  - LLM credentials for piPy (``~/.pi/agent/auth.json`` or model cards)
"""

from __future__ import annotations

import asyncio
import base64
import json
import os
import tempfile
import time
from pathlib import Path
from typing import Any

import httpx
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
MOUNTED_REL_PATH = "mnt/session/uploads/mounted.txt"
FINAL_REPLY = "mounted resources verified"

pytestmark_live = pytest.mark.live


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


def _normalize_session_events(body: dict[str, Any]) -> list[dict[str, Any]]:
    out: list[dict[str, Any]] = []
    for item in body.get("data") or []:
        inner = item.get("data")
        if isinstance(inner, dict):
            out.append(inner)
        elif isinstance(inner, str):
            out.append(json.loads(inner))
        elif isinstance(item, dict) and item.get("type"):
            out.append(item)
    return out


def _platform_url() -> str:
    explicit = os.environ.get("OMA_PLATFORM_URL", "").strip()
    if explicit:
        return explicit.rstrip("/")
    listen = os.environ.get("OMA_LISTEN_ADDR", ":8787")
    if listen.startswith(":"):
        return f"http://127.0.0.1{listen}"
    return f"http://{listen}"


def _harness_url() -> str:
    return os.environ.get("HARNESS_URL", "http://127.0.0.1:8090").rstrip("/")


def _smoke_model() -> str:
    return os.environ.get("SMOKE_MODEL", "claude-sonnet-4-6")


def _api_headers() -> dict[str, str]:
    return {"x-api-key": os.environ.get("OMA_API_KEY", "dev-key")}


def _http_client(**kwargs: Any) -> httpx.Client:
    """Local smoke tests must not route 127.0.0.1 through HTTP_PROXY."""
    return httpx.Client(trust_env=False, **kwargs)


def _require_live_platform() -> dict[str, str]:
    if os.environ.get("OMA_FAKE_HARNESS", "1") == "1":
        pytest.skip("OMA_FAKE_HARNESS=1 skips real harness — set OMA_FAKE_HARNESS=0")
    platform_url = _platform_url()
    harness_url = _harness_url()
    headers = _api_headers()
    try:
        with _http_client(timeout=5.0) as client:
            client.get(f"{platform_url}/health", headers=headers).raise_for_status()
            client.get(f"{harness_url}/health").raise_for_status()
    except httpx.HTTPError as exc:
        pytest.skip(f"platform/harness not reachable: {exc}")
    return {
        "platform_url": platform_url,
        "harness_url": harness_url,
        "model": _smoke_model(),
        "headers": headers,
    }


class PlatformSmokeClient:
    """Minimal oma-server client for resource mount live smoke."""

    def __init__(self, platform_url: str, headers: dict[str, str]) -> None:
        self.platform_url = platform_url.rstrip("/")
        self.headers = headers

    def post_json(self, path: str, body: dict[str, Any]) -> dict[str, Any]:
        with _http_client(timeout=30.0) as client:
            resp = client.post(
                f"{self.platform_url}{path}",
                headers={**self.headers, "content-type": "application/json"},
                json=body,
            )
            resp.raise_for_status()
            return resp.json()

    def get_json(self, path: str) -> dict[str, Any]:
        with _http_client(timeout=30.0) as client:
            resp = client.get(
                f"{self.platform_url}{path}",
                headers=self.headers,
            )
            resp.raise_for_status()
            return resp.json()

    def upload_text_file(self, filename: str, content: str) -> str:
        row = self.post_json(
            "/v1/files",
            {
                "filename": filename,
                "content": content,
                "media_type": "text/plain",
                "encoding": "utf8",
            },
        )
        file_id = row.get("id")
        if not isinstance(file_id, str) or not file_id:
            raise RuntimeError(f"file upload missing id: {row!r}")
        return file_id

    def create_environment(self, name: str, resources: list[dict[str, Any]]) -> str:
        row = self.post_json(
            "/v1/environments",
            {
                "name": name,
                "description": "resource mounter live smoke",
                "config": {
                    "type": "local",
                    "resources": resources,
                },
            },
        )
        env_id = row.get("id")
        if not isinstance(env_id, str) or not env_id:
            raise RuntimeError(f"environment create missing id: {row!r}")
        return env_id

    def create_agent(self, name: str, model: str, system_prompt: str) -> str:
        row = self.post_json(
            "/v1/agents",
            {
                "name": name,
                "model": model,
                "system_prompt": system_prompt,
                "tools": [
                    {
                        "type": "agent_toolset_20260401",
                        "default_config": {"enabled": False},
                        "configs": [{"name": "bash", "enabled": True}],
                    }
                ],
            },
        )
        agent_id = row.get("id")
        if not isinstance(agent_id, str) or not agent_id:
            raise RuntimeError(f"agent create missing id: {row!r}")
        return agent_id

    def create_session(
        self,
        agent_id: str,
        environment_id: str,
        title: str,
    ) -> str:
        row = self.post_json(
            "/v1/sessions",
            {
                "agent": agent_id,
                "environment_id": environment_id,
                "title": title,
            },
        )
        session_id = row.get("id")
        if not isinstance(session_id, str) or not session_id:
            raise RuntimeError(f"session create missing id: {row!r}")
        return session_id

    def send_user_message(self, session_id: str, text: str) -> None:
        self.post_json(
            f"/v1/sessions/{session_id}/events",
            {
                "events": [
                    {
                        "type": "user.message",
                        "content": [{"type": "text", "text": text}],
                    }
                ],
            },
        )

    def list_events(self, session_id: str) -> list[dict[str, Any]]:
        body = self.get_json(f"/v1/sessions/{session_id}/events?order=asc")
        return _normalize_session_events(body)

    def wait_for_resource_chain(
        self,
        session_id: str,
        *,
        timeout_sec: int,
        poll_sec: float,
    ) -> list[dict[str, Any]]:
        deadline = time.time() + timeout_sec
        last: list[dict[str, Any]] = []
        while time.time() < deadline:
            last = self.list_events(session_id)
            if _resource_mount_chain_ok(last):
                return last
            time.sleep(poll_sec)
        raise AssertionError(
            "timed out waiting for bash mount verification; "
            f"last_events={json.dumps(last, ensure_ascii=False)}"
        )

    def console_session_url(self, session_id: str) -> str:
        return f"{self.platform_url}/sessions/{session_id}"


def _resource_mount_chain_ok(events: list[dict[str, Any]]) -> bool:
    saw_bash = False
    saw_content = False
    for evt in events:
        if evt.get("type") == "session.error":
            msg = evt.get("message") or evt.get("error") or "session.error"
            raise AssertionError(f"harness turn failed: {msg}")
        if evt.get("type") == "agent.tool_use" and evt.get("name") == "bash":
            cmd = str((evt.get("input") or {}).get("command") or "")
            if MOUNTED_REL_PATH in cmd and MOUNTED_ENV in cmd:
                saw_bash = True
        if evt.get("type") == "agent.message":
            if MOUNTED_FILE_CONTENT in _message_text(evt):
                saw_content = True
    return saw_bash and saw_content


def test_resource_mounter_faux_harness_turn() -> None:
    """In-process faux piPy turn: mount_resources inside run_turn_stream."""
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
                                f"cat {MOUNTED_REL_PATH}"
                                f" && printenv {MOUNTED_ENV}"
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
                "mount_path": f"/{MOUNTED_REL_PATH}",
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
            mounted_path = Path(workdir) / MOUNTED_REL_PATH
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
    assert bash_uses, "expected bash tool_use from faux harness turn"


@pytestmark_live
def test_resource_mounter_platform_live_harness(capsys: pytest.CaptureFixture[str]) -> None:
    """Real oma-server agent/session + real harness turn with mounted resources."""
    cfg = _require_live_platform()
    client = PlatformSmokeClient(cfg["platform_url"], cfg["headers"])
    model = cfg["model"]
    timeout_sec = int(os.environ.get("SMOKE_TOOL_TIMEOUT_SEC", "180"))
    poll_sec = float(os.environ.get("SMOKE_POLL_SEC", "2"))

    file_id = client.upload_text_file("mounted.txt", MOUNTED_FILE_CONTENT)
    env_id = client.create_environment(
        f"resource-live-{int(time.time())}",
        [
            {
                "type": "file",
                "file_id": file_id,
                "mount_path": f"/{MOUNTED_REL_PATH}",
            },
            {
                "type": "env",
                "name": MOUNTED_ENV,
                "value": MOUNTED_ENV_VALUE,
            },
        ],
    )
    agent_id = client.create_agent(
        "resource-live-agent",
        model,
        (
            "You are a resource mount smoke test agent. The sandbox already has "
            "mounted files and environment variables before your turn.\n"
            "When the user asks you to verify mounts, you MUST run this exact "
            f"bash command first:\n"
            f"  cat {MOUNTED_REL_PATH} && printenv {MOUNTED_ENV}\n"
            "Then reply in one line summarizing both outputs. Never guess."
        ),
    )
    session_id = client.create_session(
        agent_id,
        env_id,
        "resource-live-smoke",
    )

    print(
        f"\nRESOURCE_LIVE_UI agent_id={agent_id} "
        f"environment_id={env_id} session_id={session_id}"
    )
    print(f"RESOURCE_LIVE_UI console={client.console_session_url(session_id)}\n")

    client.send_user_message(
        session_id,
        (
            "Verify the pre-mounted session resources. Run bash to read the "
            f"file at {MOUNTED_REL_PATH} and the {MOUNTED_ENV} environment "
            "variable, then summarize both values in one line."
        ),
    )

    events = client.wait_for_resource_chain(
        session_id,
        timeout_sec=timeout_sec,
        poll_sec=poll_sec,
    )
    assert _resource_mount_chain_ok(events)

    # Re-print after pytest capture so smoke scripts with ``pytest -s`` show URLs.
    with capsys.disabled():
        print(
            f"RESOURCE_LIVE_OK agent_id={agent_id} session_id={session_id} "
            f"console={client.console_session_url(session_id)}"
        )
