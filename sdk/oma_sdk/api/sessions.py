"""
Session Examples - High-level helper functions for session operations.
"""

from __future__ import annotations

import os
import time
from typing import TYPE_CHECKING

from anthropic import RateLimitError

if TYPE_CHECKING:
    import anthropic

_KEEP = os.getenv("OMA_KEEP_RESOURCES", "0") == "1"

# How long to wait before retrying a session-creation 429, and how many times.
# The server's sliding window is 60 s; waiting 16 s lets the oldest slot expire
# after ~4 retries in the worst case (all 5 previous sessions < 4 s old).
_SESSION_RETRY_WAIT = 16
_SESSION_RETRY_MAX = 4


class SessionExamples:
    """Example operations for sessions."""

    @staticmethod
    def _create_session(
        client: anthropic.Anthropic,
        agent_id: str,
        environment_id: str,
        *,
        title: str | None = None,
        vault_ids: list[str] | None = None,
    ):
        """Create a session, retrying up to _SESSION_RETRY_MAX times on 429."""
        for attempt in range(_SESSION_RETRY_MAX + 1):
            try:
                kwargs: dict = {
                    "agent": agent_id,
                    "environment_id": environment_id,
                }
                if title is not None:
                    kwargs["title"] = title
                if vault_ids:
                    kwargs["vault_ids"] = vault_ids
                return client.beta.sessions.create(**kwargs)
            except RateLimitError as exc:
                if "session" not in str(exc).lower() or attempt == _SESSION_RETRY_MAX:
                    raise
                print(
                    f"\n[RATE LIMIT] Session 429 — waiting {_SESSION_RETRY_WAIT}s "
                    f"for sliding window to clear (attempt {attempt + 1}/{_SESSION_RETRY_MAX})..."
                )
                time.sleep(_SESSION_RETRY_WAIT)

    @staticmethod
    def vault_ids_round_trip(
        client: anthropic.Anthropic,
        agent_id: str,
        environment_id: str,
        vault_id: str,
    ) -> dict:
        """Create a session with vault_ids and verify round-trip on retrieve."""
        sess = SessionExamples._create_session(
            client,
            agent_id,
            environment_id,
            vault_ids=[vault_id],
        )
        try:
            assert sess.id
            got = client.beta.sessions.retrieve(sess.id)
            assert got.vault_ids == [vault_id]
            return {"session": sess, "retrieved": got}
        finally:
            if not _KEEP:
                client.beta.sessions.archive(sess.id)

    @staticmethod
    def create_retrieve_and_list(
        client: anthropic.Anthropic,
        agent_id: str,
        environment_id: str
    ) -> dict:
        """Create a session, retrieve it by id, and verify it appears in the active list."""
        sess = SessionExamples._create_session(client, agent_id, environment_id)
        try:
            assert sess.id
            got = client.beta.sessions.retrieve(sess.id)
            assert got.id == sess.id
            page = client.beta.sessions.list()
            assert any(s.id == sess.id for s in page)
            return {"session": sess, "retrieved": got}
        finally:
            if not _KEEP:
                client.beta.sessions.archive(sess.id)
            else:
                print(f"\n[KEEP] session {sess.id} left active — archive manually when done")

    @staticmethod
    def send_and_list_events(
        client: anthropic.Anthropic,
        agent_id: str,
        environment_id: str
    ) -> dict:
        """Create a session, send events, and list them."""
        sess = SessionExamples._create_session(client, agent_id, environment_id)
        try:
            result = client.beta.sessions.events.send(
                sess.id,
                events=[{"type": "user.message", "content": "hello from sdk e2e"}],
            )
            assert result is not None
            events = list(client.beta.sessions.events.list(sess.id))
            assert isinstance(events, list)
            return {"session": sess, "events": events, "result": result}
        finally:
            if not _KEEP:
                client.beta.sessions.archive(sess.id)
            else:
                print(f"\n[KEEP] session {sess.id} (with events) left active — archive manually when done")

    @staticmethod
    def update_session(
        client: anthropic.Anthropic,
        agent_id: str,
        environment_id: str,
        *,
        title: str = "sdk-e2e-update-before",
        title_after: str = "sdk-e2e-update-after",
    ) -> dict:
        """Create a session, update title/metadata, and verify."""
        sess = SessionExamples._create_session(
            client, agent_id, environment_id, title=title,
        )
        try:
            updated = client.beta.sessions.update(
                sess.id,
                title=title_after,
                metadata={"sdk": "wire-compat"},
            )
            assert updated.title == title_after
            assert updated.metadata["sdk"] == "wire-compat"
            return {"session": sess, "updated": updated}
        finally:
            if not _KEEP:
                client.beta.sessions.archive(sess.id)

    @staticmethod
    def resource_crud(
        client: anthropic.Anthropic,
        agent_id: str,
        environment_id: str,
        file_id: str,
    ) -> dict:
        """Exercise sessions.resources add/list/retrieve/delete."""
        sess = SessionExamples._create_session(client, agent_id, environment_id)
        try:
            added = client.beta.sessions.resources.add(
                sess.id,
                type="file",
                file_id=file_id,
                mount_path="/workspace/sdk-resource.txt",
            )
            assert added.id
            listed = client.beta.sessions.resources.list(sess.id)
            assert any(r.id == added.id for r in listed)
            got = client.beta.sessions.resources.retrieve(
                added.id, session_id=sess.id,
            )
            assert got.id == added.id
            client.beta.sessions.resources.delete(added.id, session_id=sess.id)
            return {"session": sess, "resource": added}
        finally:
            if not _KEEP:
                client.beta.sessions.archive(sess.id)

    @staticmethod
    def thread_retrieve_and_archive(
        client: anthropic.Anthropic,
        agent_id: str,
        environment_id: str,
    ) -> dict:
        """Send sub-agent events, then retrieve and archive a thread."""
        sess = SessionExamples._create_session(client, agent_id, environment_id)
        try:
            client.beta.sessions.events.send(
                sess.id,
                events=[{
                    "type": "session.thread_created",
                    "session_thread_id": "sthr_sdk_worker",
                    "agent_id": "agt_worker",
                    "agent_name": "Worker",
                    "parent_thread_id": "sthr_primary",
                }],
            )
            thread = client.beta.sessions.threads.retrieve(
                "sthr_sdk_worker", session_id=sess.id,
            )
            assert thread.id == "sthr_sdk_worker"
            archived = client.beta.sessions.threads.archive(
                "sthr_sdk_worker", session_id=sess.id,
            )
            assert archived.status == "archived"
            return {"session": sess, "thread": thread, "archived": archived}
        finally:
            if not _KEEP:
                client.beta.sessions.archive(sess.id)

    @staticmethod
    def crud_and_events(
        client: anthropic.Anthropic,
        agent_id: str,
        environment_id: str
    ) -> dict:
        """
        Create one session and exercise retrieve, list, send-events, and list-events on it.
        Combines what would otherwise be two separate session creations, keeping total
        creations per test run to 2 (safely under the 5/60s server rate limit).

        Returns dict with keys: session, retrieved, send_result, events.
        """
        sess = SessionExamples._create_session(client, agent_id, environment_id)
        try:
            got = client.beta.sessions.retrieve(sess.id)
            assert got.id == sess.id
            page = client.beta.sessions.list()
            assert any(s.id == sess.id for s in page)

            send_result = client.beta.sessions.events.send(
                sess.id,
                events=[{"type": "user.message", "content": "hello from sdk e2e"}],
            )
            assert send_result is not None
            events = list(client.beta.sessions.events.list(sess.id))
            assert isinstance(events, list)
            return {"session": sess, "retrieved": got, "send_result": send_result, "events": events}
        finally:
            if not _KEEP:
                client.beta.sessions.archive(sess.id)
            else:
                print(f"\n[KEEP] session {sess.id} left active — archive manually when done")

    @staticmethod
    def archive_session(
        client: anthropic.Anthropic,
        agent_id: str,
        environment_id: str
    ) -> dict:
        """Create a session and archive it."""
        sess = SessionExamples._create_session(client, agent_id, environment_id)
        archived = client.beta.sessions.archive(sess.id)
        assert archived.id == sess.id
        return {"session": sess, "archived": archived}

    @staticmethod
    def delete_session(
        client: anthropic.Anthropic,
        agent_id: str,
        environment_id: str
    ) -> dict:
        """Create a session, archive it, then delete it."""
        sess = SessionExamples._create_session(client, agent_id, environment_id)
        archived = client.beta.sessions.archive(sess.id)
        deleted = client.beta.sessions.delete(sess.id)
        return {"session": sess, "archived": archived, "deleted": deleted}

    @staticmethod
    def archive_and_delete(
        client: anthropic.Anthropic,
        agent_id: str,
        environment_id: str
    ) -> dict:
        """
        Create one session, archive it (assert), then delete it.
        Combines archive + delete into a single session creation, keeping total
        creations per test run to 2 (safely under the 5/60s server rate limit).

        Returns dict with keys: session, archived.
        """
        sess = SessionExamples._create_session(client, agent_id, environment_id)
        archived = client.beta.sessions.archive(sess.id)
        assert archived.id == sess.id
        client.beta.sessions.delete(sess.id)
        return {"session": sess, "archived": archived}
