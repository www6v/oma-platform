#!/usr/bin/env python3
"""Seed team + members + mailbox row for Console team E2E."""

from __future__ import annotations

import json
import os
import sys
import time
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]
HARNESS = ROOT / "harness"
sys.path.insert(0, str(HARNESS))

from pi_team.ids import (  # noqa: E402
    PRIMARY_THREAD_ID,
    new_team_id,
    new_team_member_id,
    new_team_message_id,
    new_thread_id,
)
from pi_team.store import (  # noqa: E402
    AgentMessageRow,
    TeamMemberRow,
    TeamRow,
    TeamStore,
    resolve_database_path,
)


def main() -> int:
    session_id = os.environ.get("TEAM_E2E_SESSION_ID", "").strip()
    if not session_id:
        print("error: TEAM_E2E_SESSION_ID required", file=sys.stderr)
        return 1

    db_path = resolve_database_path(os.environ.get("DATABASE_PATH"))
    store = TeamStore(db_path)
    sess = store.get_session(session_id)
    if sess is None:
        print(f"error: session not found: {session_id}", file=sys.stderr)
        return 1

    team_name = os.environ.get("TEAM_E2E_TEAM_NAME", "console-e2e-alpha")
    mailbox_body = os.environ.get(
        "TEAM_E2E_MAILBOX_BODY",
        "TEAM-UI-MAILBOX-OK",
    )
    worker_name = os.environ.get("TEAM_E2E_WORKER_NAME", "coder")
    lead_name = os.environ.get("TEAM_E2E_LEAD_NAME", "lead")

    existing = store.get_team_by_name(session_id, team_name)
    if existing is not None:
        members = store.list_members(existing.id)
        lead = next(
            (m for m in members if m.display_name == lead_name),
            None,
        )
        worker = next(
            (m for m in members if m.display_name == worker_name),
            None,
        )
        if lead and worker:
            print(
                json.dumps(
                    {
                        "team_id": existing.id,
                        "lead_member_id": lead.id,
                        "worker_member_id": worker.id,
                        "worker_thread_id": worker.thread_id,
                        "mailbox_body": mailbox_body,
                        "reused": True,
                    }
                )
            )
            return 0

    now = int(time.time() * 1000)
    team_id = new_team_id()
    lead_member_id = new_team_member_id()
    worker_member_id = new_team_member_id()
    worker_thread_id = new_thread_id()

    store.create_team(
        TeamRow(
            id=team_id,
            session_id=session_id,
            tenant_id=sess.tenant_id,
            name=team_name,
            description="Console team tab E2E fixture",
            lead_thread_id=PRIMARY_THREAD_ID,
            lead_agent_id=sess.agent_id,
            status="active",
            created_at=now,
        )
    )
    store.create_member(
        TeamMemberRow(
            id=lead_member_id,
            team_id=team_id,
            agent_id=sess.agent_id,
            display_name=lead_name,
            color="",
            thread_id=PRIMARY_THREAD_ID,
            role="lead",
            backend_type="in_process",
            status="idle",
            joined_at=now,
        )
    )
    store.create_member(
        TeamMemberRow(
            id=worker_member_id,
            team_id=team_id,
            agent_id=f"{sess.agent_id}-worker",
            display_name=worker_name,
            color="",
            thread_id=worker_thread_id,
            role="coder",
            backend_type="in_process",
            status="listening",
            joined_at=now + 1,
        )
    )
    store.create_message(
        AgentMessageRow(
            id=new_team_message_id(),
            team_id=team_id,
            from_member_id=lead_member_id,
            to_member_id=worker_member_id,
            message_type="text",
            body=mailbox_body,
            summary="",
            read_at=None,
            created_at=now + 2,
        )
    )

    print(
        json.dumps(
            {
                "team_id": team_id,
                "lead_member_id": lead_member_id,
                "worker_member_id": worker_member_id,
                "worker_thread_id": worker_thread_id,
                "mailbox_body": mailbox_body,
                "reused": False,
            }
        )
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
