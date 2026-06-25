"""
Sub-agent Examples — helpers for multi-agent coordinator setup and observability.

Reference: open-managed-agents ``test/eval/runner.ts`` (subAgents → callable_agents)
and oma-platform ``docs/design/subagent.md``.
"""

from __future__ import annotations

import json
import os
import time
from typing import TYPE_CHECKING, Any
from urllib.parse import urlparse

import httpx

from oma_sdk.subagent import (
    DEFAULT_MODEL,
    DEFAULT_TOOLS,
    DEMO_COORDINATOR_NAME,
    DEMO_ENV_NAME,
    DEMO_SESSION_TITLE,
    DEMO_WORKER_NAME,
    WORKER_REPLY_MARKER,
    SubAgentConfig,
    build_general_subagent_metadata,
    build_multiagent,
    build_role_metadata,
    count_thread_created,
    events_of_type,
    event_type,
    extract_event_data,
)

if TYPE_CHECKING:
    import anthropic

_KEEP = os.getenv("OMA_KEEP_RESOURCES", "0") == "1"
_DEMO_TIMEOUT_SEC = int(os.getenv("SUBAGENT_DEMO_TIMEOUT_SEC", "180"))
_DEMO_POLL_SEC = float(os.getenv("SUBAGENT_DEMO_POLL_SEC", "2"))


def _console_base_url() -> str:
    """Derive Console base URL from OMA_BASE_URL (API and UI share host)."""
    explicit = os.getenv("OMA_CONSOLE_URL")
    if explicit:
        return explicit.rstrip("/")
    api_base = os.getenv("OMA_BASE_URL", "http://localhost:8787").rstrip("/")
    parsed = urlparse(api_base)
    if parsed.path and parsed.path not in ("", "/"):
        return f"{parsed.scheme}://{parsed.netloc}"
    return api_base


def _call_agent_tool_prefix(worker_id: str) -> str:
    safe = worker_id.replace("-", "_")
    return f"call_agent_{safe}"


class SubagentExamples:
    """Example operations for sub-agent configuration and session threads."""

    @staticmethod
    def create_worker(
        client: anthropic.Anthropic,
        *,
        name: str = "sdk-e2e-subagent-worker",
        system: str = "You are a worker sub-agent. Answer concisely.",
    ):
        """Create a worker agent that can be referenced from a coordinator."""
        return client.beta.agents.create(
            name=name,
            model=DEFAULT_MODEL,
            system=system,
            tools=DEFAULT_TOOLS,
        )

    @staticmethod
    def create_coordinator(
        client: anthropic.Anthropic,
        worker_ids: list[str],
        *,
        name: str = "sdk-e2e-subagent-coordinator",
        system: str = (
            "You are a coordinator. Delegate specialized work to sub-agents "
            "using call_agent tools."
        ),
        worker_versions: dict[str, int] | None = None,
    ):
        """Create a coordinator agent with ``multiagent`` roster."""
        return client.beta.agents.create(
            name=name,
            model=DEFAULT_MODEL,
            system=system,
            tools=DEFAULT_TOOLS,
            multiagent=build_multiagent(
                worker_ids,
                versions=worker_versions,
            ),
        )

    @staticmethod
    def create_with_general_subagent(
        client: anthropic.Anthropic,
        *,
        name: str = "sdk-e2e-general-subagent",
        system: str = (
            "Use general_subagent for focused sub-tasks that need a clean context."
        ),
    ) -> dict[str, Any]:
        """Create an agent with ``enable_general_subagent`` via raw POST."""
        resp = client.post(
            "/v1/agents",
            cast_to=httpx.Response,
            body={
                "name": name,
                "model": DEFAULT_MODEL,
                "system": system,
                "tools": DEFAULT_TOOLS,
                "metadata": build_general_subagent_metadata(),
            },
        )
        data = resp.json()
        assert data.get("id")
        return data

    @staticmethod
    def create_with_role_defaults(
        client: anthropic.Anthropic,
        roles: dict[str, str],
        *,
        name: str = "sdk-e2e-role-coordinator",
        system: str = "Coordinate explore / plan / verify roles as needed.",
    ) -> dict[str, Any]:
        """Create a coordinator with ``default_subagent_roles`` metadata."""
        resp = client.post(
            "/v1/agents",
            cast_to=httpx.Response,
            body={
                "name": name,
                "model": DEFAULT_MODEL,
                "system": system,
                "tools": DEFAULT_TOOLS,
                "metadata": build_role_metadata(roles),
            },
        )
        data = resp.json()
        assert data.get("id")
        return data

    @staticmethod
    def create_workers_from_config(
        client: anthropic.Anthropic,
        sub_agents: list[SubAgentConfig],
    ) -> list[Any]:
        """Create worker agents from eval-style ``subAgents`` configs."""
        created = []
        for sub in sub_agents:
            model = sub.get("model", DEFAULT_MODEL)
            tools = sub.get("tools", DEFAULT_TOOLS)
            agent = client.beta.agents.create(
                name=sub.get("name", "sdk-e2e-subagent-worker"),
                model=model,
                system=sub.get(
                    "system",
                    "You are a worker sub-agent.",
                ),
                tools=tools,
            )
            created.append(agent)
        return created

    @staticmethod
    def setup_coordinator_session(
        client: anthropic.Anthropic,
        environment_id: str,
        sub_agents: list[SubAgentConfig] | None = None,
        *,
        coordinator_system: str | None = None,
    ) -> dict[str, Any]:
        """
        End-to-end setup: workers → coordinator → session.

        Mirrors open-managed-agents eval runner subAgents wiring.
        """
        from .sessions import SessionExamples

        sub_agents = sub_agents or [
            {
                "name": "sdk-e2e-researcher",
                "system": (
                    "You are a concise research assistant. "
                    "Answer questions directly."
                ),
            },
        ]
        workers = SubagentExamples.create_workers_from_config(
            client,
            sub_agents,
        )
        worker_ids = [w.id for w in workers]
        versions = {w.id: w.version for w in workers}
        coordinator = SubagentExamples.create_coordinator(
            client,
            worker_ids,
            worker_versions=versions,
            system=coordinator_system
            or (
                "You are a coordinator. Delegate to sub-agents using "
                "call_agent tools when needed."
            ),
        )
        session = SessionExamples._create_session(
            client,
            coordinator.id,
            environment_id,
        )
        return {
            "workers": workers,
            "coordinator": coordinator,
            "session": session,
        }

    @staticmethod
    def verify_coordinator_multiagent(
        client: anthropic.Anthropic,
        coordinator_id: str,
        expected_worker_ids: list[str],
    ) -> dict[str, Any]:
        """Retrieve coordinator and assert multiagent roster matches."""
        got = client.beta.agents.retrieve(coordinator_id)
        assert got.multiagent is not None
        assert got.multiagent.type == "coordinator"
        roster_ids = [entry.id for entry in got.multiagent.agents]
        for worker_id in expected_worker_ids:
            assert worker_id in roster_ids
        return {"agent": got, "roster_ids": roster_ids}

    @staticmethod
    def list_threads(
        client: anthropic.Anthropic,
        session_id: str,
    ) -> list[dict[str, Any]]:
        """GET /v1/sessions/{id}/threads — primary + sub-agent threads."""
        resp = client.get(
            f"/v1/sessions/{session_id}/threads",
            cast_to=httpx.Response,
        )
        data = resp.json()
        threads = data.get("data", [])
        assert isinstance(threads, list)
        return threads

    @staticmethod
    def get_trajectory(
        client: anthropic.Anthropic,
        session_id: str,
    ) -> dict[str, Any]:
        """GET /v1/sessions/{id}/trajectory — includes ``num_threads`` summary."""
        resp = client.get(
            f"/v1/sessions/{session_id}/trajectory",
            cast_to=httpx.Response,
        )
        data = resp.json()
        assert data.get("schema_version") == "oma.trajectory.v1"
        return data

    @staticmethod
    def create_coordinator_and_verify(
        client: anthropic.Anthropic,
        *,
        worker_name: str = "sdk-e2e-subagent-worker",
        coordinator_name: str = "sdk-e2e-subagent-coordinator",
    ) -> dict[str, Any]:
        """Create worker + coordinator, verify multiagent, archive unless KEEP."""
        worker = SubagentExamples.create_worker(
            client,
            name=worker_name,
        )
        coordinator = SubagentExamples.create_coordinator(
            client,
            [worker.id],
            name=coordinator_name,
            worker_versions={worker.id: worker.version},
        )
        try:
            verified = SubagentExamples.verify_coordinator_multiagent(
                client,
                coordinator.id,
                [worker.id],
            )
            return {
                "worker": worker,
                "coordinator": coordinator,
                **verified,
            }
        finally:
            if not _KEEP:
                client.beta.agents.archive(coordinator.id)
                client.beta.agents.archive(worker.id)
            else:
                print(
                    f"\n[KEEP] coordinator {coordinator.id}, "
                    f"worker {worker.id} — archive manually when done",
                )

    @staticmethod
    def session_threads_and_trajectory(
        client: anthropic.Anthropic,
        agent_id: str,
        environment_id: str,
    ) -> dict[str, Any]:
        """Create a session and exercise threads + trajectory endpoints."""
        from .sessions import SessionExamples

        sess = SessionExamples._create_session(
            client,
            agent_id,
            environment_id,
        )
        try:
            threads = SubagentExamples.list_threads(client, sess.id)
            assert len(threads) >= 1
            assert threads[0].get("id") == "sthr_primary"

            trajectory = SubagentExamples.get_trajectory(client, sess.id)
            summary = trajectory.get("summary", {})
            assert summary.get("num_threads", 0) >= 1
            return {
                "session": sess,
                "threads": threads,
                "trajectory": trajectory,
            }
        finally:
            if not _KEEP:
                client.beta.sessions.archive(sess.id)
            else:
                print(
                    f"\n[KEEP] session {sess.id} — archive manually when done",
                )

    @staticmethod
    def fetch_session_events(
        client: anthropic.Anthropic,
        session_id: str,
    ) -> list[dict[str, Any]]:
        """Fetch normalized session events (wire ``data`` payloads)."""
        resp = client.get(
            f"/v1/sessions/{session_id}/events?order=asc&limit=500",
            cast_to=httpx.Response,
        )
        body = resp.json()
        normalized: list[dict[str, Any]] = []
        for item in body.get("data", []):
            inner = item.get("data")
            if isinstance(inner, dict):
                normalized.append(inner)
            elif isinstance(inner, str):
                normalized.append(json.loads(inner))
            elif isinstance(item, dict) and item.get("type"):
                normalized.append(item)
        return normalized

    @staticmethod
    def wait_for_delegation(
        client: anthropic.Anthropic,
        session_id: str,
        worker_id: str,
        *,
        timeout_sec: int = _DEMO_TIMEOUT_SEC,
        poll_sec: float = _DEMO_POLL_SEC,
        worker_marker: str = WORKER_REPLY_MARKER,
    ) -> list[dict[str, Any]]:
        """
        Poll until call_agent delegation completes or timeout.

        Mirrors ``scripts/multi-agent/smoke-subagent-live-e2e.sh``.
        """
        deadline = time.time() + timeout_sec
        last_events: list[dict[str, Any]] = []
        tool_prefix = _call_agent_tool_prefix(worker_id)

        while time.time() < deadline:
            last_events = SubagentExamples.fetch_session_events(
                client,
                session_id,
            )
            status = SubagentExamples._delegation_chain_status(
                last_events,
                tool_prefix=tool_prefix,
                worker_marker=worker_marker,
            )
            if status == "done":
                return last_events
            if status == "error":
                err = next(
                    (
                        ev.get("message") or ev.get("error") or "session.error"
                        for ev in last_events
                        if ev.get("type") == "session.error"
                    ),
                    "session.error",
                )
                raise RuntimeError(f"session turn failed: {err}")

            time.sleep(poll_sec)

        raise TimeoutError(
            f"timed out after {timeout_sec}s waiting for sub-agent delegation "
            f"(session={session_id})",
        )

    @staticmethod
    def _delegation_chain_status(
        events: list[dict[str, Any]],
        *,
        tool_prefix: str,
        worker_marker: str,
    ) -> str:
        """Return ``done``, ``pending``, or ``error``."""
        saw_tool = False
        saw_thread = False
        saw_worker_msg = False
        saw_primary = False

        for ev in events:
            if ev.get("type") == "session.error":
                return "error"
            name = ev.get("name") or ""
            if ev.get("type") == "agent.tool_use" and name.startswith("call_agent_"):
                saw_tool = True
            if ev.get("type") == "session.thread_created":
                saw_thread = True
            if ev.get("type") == "agent.message":
                text = SubagentExamples._message_text(ev)
                if ev.get("session_thread_id"):
                    if worker_marker in text:
                        saw_worker_msg = True
                elif text.strip():
                    saw_primary = True

        if saw_tool and saw_thread and saw_worker_msg and saw_primary:
            return "done"
        return "pending"

    @staticmethod
    def _message_text(event: dict[str, Any]) -> str:
        parts: list[str] = []
        for block in event.get("content") or []:
            if block.get("type") == "text" and block.get("text"):
                parts.append(str(block["text"]))
        return "\n".join(parts).strip()

    @staticmethod
    def print_console_links(
        *,
        coordinator_id: str,
        worker_id: str,
        session_id: str,
        threads: list[dict[str, Any]] | None = None,
        base_url: str | None = None,
    ) -> None:
        """Print Console URLs so you can inspect agents and sub-agent threads."""
        root = (base_url or _console_base_url()).rstrip("/")
        print("\n=== SDK Subagent Demo — open in Console ===")
        print(f"  Coordinator agent : {root}/agents/{coordinator_id}")
        print(f"  Worker agent      : {root}/agents/{worker_id}")
        print(f"  Session (threads) : {root}/sessions/{session_id}")
        if threads:
            print(f"  Thread count      : {len(threads)}")
            for thread in threads:
                tid = thread.get("id", "?")
                label = thread.get("agent_name") or thread.get("agent_id") or ""
                status = thread.get("status") or ""
                print(f"    - {tid}  {label}  ({status})")
        print(
            "\n  In Session detail, switch thread tabs to view sub-agent "
            "tool calls and messages.",
        )
        print(
            "  Cleanup later: OMA_KEEP_RESOURCES=0 or "
            "scripts/clean-all/clean_user_resources.py\n",
        )

    @staticmethod
    def create_demo_environment(client: anthropic.Anthropic):
        """Create a recognizable environment for the sub-agent demo."""
        return client.beta.environments.create(name=DEMO_ENV_NAME)

    @staticmethod
    def run_live_delegation_demo(
        client: anthropic.Anthropic,
        environment_id: str | None = None,
        *,
        keep_resources: bool | None = None,
        timeout_sec: int = _DEMO_TIMEOUT_SEC,
    ) -> dict[str, Any]:
        """
        Full live demo: worker + coordinator + session + real delegation turn.

        Creates resources with fixed names (``sdk-subagent-*``) so they are
        easy to find in the Console Agents / Sessions lists. By default keeps
        all resources after the run for UI inspection.
        """
        from .sessions import SessionExamples

        keep = _KEEP if keep_resources is None else keep_resources
        env = None
        if environment_id is None:
            env = SubagentExamples.create_demo_environment(client)
            environment_id = env.id

        worker = SubagentExamples.create_worker(
            client,
            name=DEMO_WORKER_NAME,
            system=(
                "You are a worker sub-agent for SDK demo. For every task, "
                f"respond with exactly {WORKER_REPLY_MARKER} and nothing else."
            ),
        )
        coordinator = SubagentExamples.create_coordinator(
            client,
            [worker.id],
            name=DEMO_COORDINATOR_NAME,
            worker_versions={worker.id: worker.version},
            system=(
                "You are a coordinator for SDK sub-agent demo. You MUST delegate "
                "every user request to the worker using the call_agent tool "
                "(call_agent_*). Never answer directly without delegating first. "
                "After you receive the worker tool result, reply with a one-line "
                "summary that includes the worker text."
            ),
        )
        session = SessionExamples._create_session(
            client,
            coordinator.id,
            environment_id,
            title=DEMO_SESSION_TITLE,
        )

        client.beta.sessions.events.send(
            session.id,
            events=[
                {
                    "type": "user.message",
                    "content": [
                        {
                            "type": "text",
                            "text": (
                                "Delegate to the worker with message: perform "
                                "SDK smoke task. After delegation, summarize "
                                "the worker result in one line."
                            ),
                        },
                    ],
                },
            ],
        )

        events = SubagentExamples.wait_for_delegation(
            client,
            session.id,
            worker.id,
            timeout_sec=timeout_sec,
        )

        threads = SubagentExamples.list_threads(client, session.id)
        trajectory = SubagentExamples.get_trajectory(client, session.id)
        thread_count = len(events_of_type(events, "session.thread_created"))
        num_threads = trajectory.get("summary", {}).get("num_threads", 0)

        assert thread_count >= 1, "expected session.thread_created"
        assert len(threads) >= 2, (
            f"expected primary + sub threads, got {len(threads)}"
        )
        assert num_threads >= 2, f"expected num_threads >= 2, got {num_threads}"

        SubagentExamples.print_console_links(
            coordinator_id=coordinator.id,
            worker_id=worker.id,
            session_id=session.id,
            threads=threads,
        )

        if keep:
            print(
                f"[KEEP] demo resources left active "
                f"(coordinator={coordinator.id}, worker={worker.id}, "
                f"session={session.id})",
            )
        else:
            client.beta.sessions.archive(session.id)
            client.beta.agents.archive(coordinator.id)
            client.beta.agents.archive(worker.id)
            if env is not None:
                try:
                    client.beta.environments.archive(env.id)
                except Exception:
                    pass

        return {
            "worker": worker,
            "coordinator": coordinator,
            "session": session,
            "environment_id": environment_id,
            "events": events,
            "threads": threads,
            "trajectory": trajectory,
            "thread_created_count": thread_count,
        }


# Re-export event helpers for convenience in tests and downstream code.
__all__ = [
    "SubagentExamples",
    "count_thread_created",
    "events_of_type",
    "event_type",
    "extract_event_data",
]
