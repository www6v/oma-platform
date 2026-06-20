"""E2E tests for /v1/sessions — CRUD, events."""

from __future__ import annotations

import anthropic


def test_sessions_create_and_retrieve(client: anthropic.Anthropic, tmp_agent, tmp_env):
    sess = client.beta.sessions.create(agent=tmp_agent.id, environment_id=tmp_env)
    try:
        assert sess.id
        got = client.beta.sessions.retrieve(sess.id)
        assert got.id == sess.id
    finally:
        client.beta.sessions.archive(sess.id)


def test_sessions_list(client: anthropic.Anthropic):
    page = client.beta.sessions.list()
    assert isinstance(list(page), list)


def test_sessions_events_send_and_list(client: anthropic.Anthropic, tmp_agent, tmp_env):
    sess = client.beta.sessions.create(agent=tmp_agent.id, environment_id=tmp_env)
    try:
        result = client.beta.sessions.events.send(
            sess.id,
            events=[{"type": "user.message", "content": "hello from sdk e2e"}],
        )
        assert result is not None

        page = client.beta.sessions.events.list(sess.id)
        events = list(page)
        assert isinstance(events, list)
    finally:
        client.beta.sessions.archive(sess.id)


def test_sessions_archive(client: anthropic.Anthropic, tmp_agent, tmp_env):
    sess = client.beta.sessions.create(agent=tmp_agent.id, environment_id=tmp_env)
    archived = client.beta.sessions.archive(sess.id)
    assert archived.id == sess.id


def test_sessions_delete(client: anthropic.Anthropic, tmp_agent, tmp_env):
    sess = client.beta.sessions.create(agent=tmp_agent.id, environment_id=tmp_env)
    client.beta.sessions.archive(sess.id)
    client.beta.sessions.delete(sess.id)
