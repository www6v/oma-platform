"""E2E tests for /v1/sessions — CRUD, events, stream."""

from __future__ import annotations

import pytest
from oma_sdk import OMAClient


async def test_sessions_create_and_retrieve(client: OMAClient, tmp_agent, tmp_env):
    sess = client.sessions.create(agent=tmp_agent.id, environment_id=tmp_env)
    try:
        assert sess.id
        got = client.sessions.retrieve(sess.id)
        assert got.id == sess.id
    finally:
        client.sessions.archive(sess.id)


async def test_sessions_list(client: OMAClient):
    page = client.sessions.list()
    assert isinstance(list(page), list)


async def test_sessions_events_send_and_list(client: OMAClient, tmp_agent, tmp_env):
    sess = client.sessions.create(agent=tmp_agent.id, environment_id=tmp_env)
    try:
        result = client.sessions.events.send(
            sess.id,
            events=[{"type": "user.message", "content": "hello from sdk e2e"}],
        )
        assert result is not None

        page = client.sessions.events.list(sess.id)
        events = list(page)
        assert isinstance(events, list)
    finally:
        client.sessions.archive(sess.id)


async def test_sessions_archive(client: OMAClient, tmp_agent, tmp_env):
    sess = client.sessions.create(agent=tmp_agent.id, environment_id=tmp_env)
    archived = client.sessions.archive(sess.id)
    assert archived.id == sess.id


async def test_sessions_delete(client: OMAClient, tmp_agent, tmp_env):
    sess = client.sessions.create(agent=tmp_agent.id, environment_id=tmp_env)
    client.sessions.archive(sess.id)
    client.sessions.delete(sess.id)


async def test_sessions_messages_endpoint(client: OMAClient, tmp_agent, tmp_env):
    sess = client.sessions.create(agent=tmp_agent.id, environment_id=tmp_env)
    try:
        r = await client._http.post(
            f"/v1/sessions/{sess.id}/messages",
            json={"role": "user", "content": "hello from messages endpoint"},
        )
        assert r.status_code == 202
        assert r.json()["status"] == "queued"
    finally:
        client.sessions.archive(sess.id)
