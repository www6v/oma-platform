"""OG5 live soak runner — EV fast-charging outcome grader cookbook."""

from __future__ import annotations

import io
import os
from typing import Any

from oma_sdk import OMAClient, StreamConfig, stream_until_end_turn, wait_for_idle_status

from ev_charging_fixtures import (
    AGENT_NAME,
    BRIEF_FILENAME,
    ENV_NAME,
    MAX_ITERATIONS,
    SESSION_TITLE,
    WRITER_TOOLS,
    build_kickoff_message,
    load_rubric,
    load_system_prompt,
    load_task,
)


def _stream_config() -> StreamConfig:
    return StreamConfig(
        timeout_sec=float(os.getenv("OMA_EV_TIMEOUT_SEC", "1800")),
        stream_read_timeout=float(
            os.getenv("OMA_STREAM_READ_TIMEOUT_SEC", "600"),
        ),
        idle_poll_max_wait=float(
            os.getenv("OMA_IDLE_POLL_MAX_WAIT_SEC", "60"),
        ),
    )


def _build_define_outcome(
    task: str,
    rubric_file_id: str | None,
    rubric_text: str,
) -> dict[str, Any]:
    if rubric_file_id:
        rubric: dict[str, Any] | str = {
            "type": "file",
            "file_id": rubric_file_id,
        }
    else:
        rubric = {"type": "text", "content": rubric_text}
    return {
        "type": "user.define_outcome",
        "description": task,
        "rubric": rubric,
        "max_iterations": MAX_ITERATIONS,
    }


async def _session_has_brief(client: OMAClient, session_id: str) -> bool:
    outputs = await client.files.list(scope_id=session_id)
    files = outputs.get("data") or []
    return any(f.get("filename") == BRIEF_FILENAME for f in files)


def _collect_outcome_events_from_list(
    rows: list[dict[str, Any]],
) -> list[dict[str, Any]]:
    out: list[dict[str, Any]] = []
    for row in rows:
        payload = row.get("data")
        if not isinstance(payload, dict):
            payload = row
        if payload.get("type") == "span.outcome_evaluation_end":
            out.append(payload)
    return out


async def _list_session_events(client: OMAClient, session_id: str) -> list[dict]:
    listed = await client.events.list(session_id, order="asc", limit=5000)
    return listed.get("data") or []


async def run_ev_charging_outcome_soak(
    client: OMAClient,
    *,
    model: str,
    use_file_rubric: bool = True,
    keep_resources: bool = False,
) -> dict[str, Any]:
    """Run the full EV charging outcome grader soak (OG5).

    Returns a summary dict with session id, terminal outcome result, and
    whether ``brief.md`` was written to session outputs.
    """
    task = load_task()
    rubric_text = load_rubric()
    system_prompt = load_system_prompt()

    env = client.environments.create(
        name=ENV_NAME,
        config={"type": "cloud", "networking": {"type": "unrestricted"}},
    )
    agent = client.agents.create(
        name=AGENT_NAME,
        model={"id": model},
        system=system_prompt,
        tools=WRITER_TOOLS,
    )
    session = client.sessions.create(
        environment_id=env.id,
        agent={"type": "agent", "id": agent.id, "version": agent.version},
        title=SESSION_TITLE,
    )

    rubric_file_id: str | None = None
    if use_file_rubric:
        uploaded = await client.files.upload(
            file=(
                "ev-charging-rubric.md",
                io.BytesIO(rubric_text.encode("utf-8")),
                "text/markdown",
            ),
            downloadable=True,
        )
        rubric_file_id = uploaded["id"]

    events_to_send = [
        _build_define_outcome(task, rubric_file_id, rubric_text),
        {
            "type": "user.message",
            "content": [{"type": "text", "text": build_kickoff_message(task)}],
        },
    ]

    streamed_events: list[dict[str, Any]] = []

    def _on_event(ev: dict[str, Any]) -> None:
        streamed_events.append(ev)

    cfg = _stream_config()
    await stream_until_end_turn(
        client,
        session.id,
        send_events=events_to_send,
        config=cfg,
        on_event=_on_event,
    )
    await wait_for_idle_status(
        client,
        session.id,
        max_wait=cfg.idle_poll_max_wait,
    )

    event_rows = await _list_session_events(client, session.id)
    detail = client.sessions.retrieve(session.id)
    evals = getattr(detail, "outcome_evaluations", None) or []
    outcome_ends = _collect_outcome_events_from_list(event_rows)
    terminal = outcome_ends[-1]["result"] if outcome_ends else None
    if not terminal and evals:
        last = evals[-1]
        terminal = (
            last.get("result")
            if isinstance(last, dict)
            else getattr(last, "result", None)
        )

    has_brief = await _session_has_brief(client, session.id)

    if not keep_resources and os.getenv("OMA_KEEP_RESOURCES") != "1":
        client.sessions.archive(session.id)
        client.agents.archive(agent.id)
        client.environments.archive(env.id)

    return {
        "session_id": session.id,
        "agent_id": agent.id,
        "environment_id": env.id,
        "terminal_result": terminal,
        "outcome_pass_count": len(outcome_ends),
        "outcome_evaluations": evals,
        "has_brief": has_brief,
        "stream_event_count": len(streamed_events),
    }
