"""
Sub-agent Examples — helpers for multi-agent coordinator setup and observability.

Reference: open-managed-agents ``test/eval/runner.ts`` (subAgents → callable_agents)
and oma-platform ``docs/design/subagent.md``.
"""

from __future__ import annotations

import os
from typing import TYPE_CHECKING, Any

import httpx

from oma_sdk.subagent import (
    DEFAULT_MODEL,
    DEFAULT_TOOLS,
    SubAgentConfig,
    build_general_subagent_metadata,
    build_multiagent,
    build_role_metadata,
    count_thread_created,
    events_of_type,
)

if TYPE_CHECKING:
    import anthropic

_KEEP = os.getenv("OMA_KEEP_RESOURCES", "0") == "1"


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


# Re-export event helpers for convenience in tests and downstream code.
__all__ = [
    "SubagentExamples",
    "count_thread_created",
    "events_of_type",
]
