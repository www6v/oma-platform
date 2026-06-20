#!/usr/bin/env python3
"""Shared Python utilities for E2E smoke tests.

Each subcommand reads JSON from stdin (unless it only builds a body),
writes output to stdout, and exits non-zero on assertion failure.

Exit-code contracts for check-events-* commands:
  0  – desired state reached
  1  – fake-harness mode detected (evt_fake / claude not logged in)
  2  – not ready yet (keep polling)
  3  – session.error event received
  4  – workdir missing (bash smoke only)

Usage:
  python3 smoke_utils.py <command> [args...]
"""

from __future__ import annotations

import json
import os
import re
import sys
import time
import uuid


# ─── helpers ───────────────────────────────────────────────────────────────

def _load() -> dict | list:
    return json.load(sys.stdin)


def _die(msg: str, code: int = 1) -> None:
    print(msg, file=sys.stderr)
    sys.exit(code)


# ─── JSON utilities ─────────────────────────────────────────────────────────

def cmd_json_field(args: list[str]) -> None:
    """Extract a top-level field from stdin JSON and print it."""
    if not args:
        _die("usage: json-field <field>")
    field = args[0]
    data = _load()
    if field not in data:
        _die(f"field {field!r} not in JSON: {data!r}")
    print(data[field])


def cmd_normalize_events(_args: list[str]) -> None:
    """Normalize AMA events envelope.

    Unwraps the inner data payload and propagates the outer 'type' field
    when the inner event does not have one.
    """
    raw = _load()
    out = []
    for item in raw.get("data", []):
        typ = item.get("type")
        inner = item.get("data")
        if isinstance(inner, dict):
            evt = dict(inner)
            if typ and "type" not in evt:
                evt["type"] = typ
            out.append(evt)
        elif isinstance(inner, str):
            evt = json.loads(inner)
            if typ and "type" not in evt:
                evt["type"] = typ
            out.append(evt)
        else:
            out.append(item)
    print(json.dumps({"data": out}))


# ─── /v1/me and /v1/stats ──────────────────────────────────────────────────

def cmd_check_me(_args: list[str]) -> None:
    """Validate /v1/me response and print tenant/user IDs."""
    me = _load()
    tenant = me.get("tenant")
    if not isinstance(tenant, dict) or not tenant.get("id"):
        _die(f"missing tenant: {me!r}")
    tid = tenant.get("id")
    uid = (me.get("user") or {}).get("id", "?")
    print(f"tenant={tid!r} user={uid!r}")


def cmd_check_stats(_args: list[str]) -> None:
    """Validate /v1/stats response and print counts."""
    stats = _load()
    for key in ("agents", "sessions", "environments", "skills", "model_cards"):
        if key not in stats:
            _die(f"missing stats.{key}")
    print(
        "agents=%d sessions=%d environments=%d skills=%d model_cards=%d"
        % (
            stats["agents"],
            stats["sessions"],
            stats["environments"],
            stats["skills"],
            stats["model_cards"],
        )
    )


def cmd_check_stats_after(_args: list[str]) -> None:
    """Validate /v1/stats has at least 1 agent and 1 session."""
    stats = _load()
    if stats.get("agents", 0) < 1 or stats.get("sessions", 0) < 1:
        _die(f"unexpected stats after smoke: {stats!r}")
    print(
        "stats_after agents=%d sessions=%d skills=%d vaults=%d"
        % (
            stats.get("agents", 0),
            stats.get("sessions", 0),
            stats.get("skills", 0),
            stats.get("vaults", 0),
        )
    )


# ─── environments ───────────────────────────────────────────────────────────

def cmd_check_environments(args: list[str]) -> None:
    """Validate environments list and confirm default env is present."""
    if not args:
        _die("usage: check-environments <default_env_id>")
    default_id = args[0]
    data = _load()["data"]
    ids = {row["id"] for row in data}
    if default_id not in ids:
        _die(f"missing default env {default_id!r}, got {sorted(ids)}")
    print(f"environments={len(data)} default_ok")


def cmd_check_environment(args: list[str]) -> None:
    """Validate a single environment response."""
    if not args:
        _die("usage: check-environment <env_id>")
    env_id = args[0]
    env = _load()
    if env.get("id") != env_id:
        _die(f"env id mismatch: {env!r}")
    cfg = env.get("config") or {}
    typ = cfg.get("type") if isinstance(cfg, dict) else None
    print("name=%s type=%s" % (env.get("name"), typ))


# ─── skills ─────────────────────────────────────────────────────────────────

def cmd_check_skills(_args: list[str]) -> None:
    """Validate skills list: >=4 entries, builtin_pdf present with correct source."""
    data = _load().get("data", [])
    if len(data) < 4:
        _die(f"expected >=4 builtin skills, got {len(data)}")
    builtin = next((s for s in data if s.get("id") == "builtin_pdf"), None)
    if not builtin or builtin.get("source") != "anthropic":
        _die(f"builtin_pdf missing or wrong source: {builtin!r}")
    print(f"skills={len(data)} builtin_pdf_ok")


def cmd_check_skill_versions(_args: list[str]) -> None:
    """Validate skill versions list has at least one entry."""
    data = _load().get("data", [])
    if len(data) < 1:
        _die("expected >=1 skill version")
    print(f"skill_versions={len(data)}")


def cmd_make_skill_body(_args: list[str]) -> None:
    """Build skill POST body JSON with a timestamped name."""
    body = {
        "name": "smoke-skill-" + str(int(time.time())),
        "display_title": "Smoke Skill",
        "description": "smoke-test.sh custom skill",
        "files": [
            {
                "filename": "SKILL.md",
                "content": "---\nname: smoke-skill\ndescription: smoke\n---\n# Smoke",
            }
        ],
    }
    print(json.dumps(body))


# ─── vaults / credentials ───────────────────────────────────────────────────

def cmd_check_credential(_args: list[str]) -> None:
    """Validate credential response: access_token must be absent, id must be present."""
    cred = _load()
    auth = cred.get("auth") or {}
    if auth.get("access_token") is not None:
        _die("access_token must be stripped from API response")
    if not cred.get("id"):
        _die(f"missing credential id: {cred!r}")
    cid = cred.get("id")
    print(f"credential={cid!r} redacted_ok")


# ─── model cards ────────────────────────────────────────────────────────────

def cmd_count_model_cards(_args: list[str]) -> None:
    """Print number of model cards."""
    data = _load()
    print(len(data["data"]))


def cmd_make_model_card_body(args: list[str]) -> None:
    """Build model card POST body (requires ANTHROPIC_API_KEY env var)."""
    if len(args) < 2:
        _die("usage: make-model-card-body <model_id> <model>")
    model_id, model = args[0], args[1]
    api_key = os.environ.get("ANTHROPIC_API_KEY", "")
    body = {
        "model_id": model_id,
        "model": model,
        "provider": "ant",
        "api_key": api_key,
        "is_default": True,
    }
    print(json.dumps(body))


def cmd_find_model_card(args: list[str]) -> None:
    """Find a model card row ID by model_id from a list response."""
    if not args:
        _die("usage: find-model-card <model_id>")
    target = args[0]
    data = _load()
    for row in data.get("data", []):
        if row.get("model_id") == target:
            print(row["id"])
            sys.exit(0)
    _die(f"model_id {target!r} conflict but not in list")


def cmd_make_model_card_update_body(args: list[str]) -> None:
    """Build model card PATCH/update body (requires ANTHROPIC_API_KEY env var)."""
    if not args:
        _die("usage: make-model-card-update-body <model>")
    model = args[0]
    api_key = os.environ.get("ANTHROPIC_API_KEY", "")
    body = {
        "model": model,
        "provider": "ant",
        "api_key": api_key,
        "is_default": True,
    }
    print(json.dumps(body))


def cmd_check_key_preview(_args: list[str]) -> None:
    """Print API key status: 'set len=N' or 'empty len=0'."""
    data = _load()
    k = data.get("api_key", "")
    print("set" if k else "empty", f"len={len(k)}")


def cmd_list_model_card_ids(_args: list[str]) -> None:
    """Print all model card IDs, one per line (for deletion loop)."""
    data = _load()
    for row in data.get("data", []):
        print(row["id"])


# ─── agents ─────────────────────────────────────────────────────────────────

def cmd_make_agent_body(args: list[str]) -> None:
    """Build agent POST body JSON."""
    if not args:
        _die("usage: make-agent-body <model>")
    model = args[0]
    body = {
        "name": "hello",
        "model": model,
        "system_prompt": "You are helpful.",
        "description": "smoke test agent",
        "tools": [{"type": "agent_toolset_20260401"}],
    }
    print(json.dumps(body))


def cmd_make_mcp_agent_body(args: list[str]) -> None:
    """Build MCP agent POST body JSON."""
    if not args:
        _die("usage: make-mcp-agent-body <model>")
    model = args[0]
    mcp_url = os.environ.get("MCP_MOCK_URL", "")
    body = {
        "name": "smoke-mcp-agent",
        "model": model,
        "system_prompt": (
            "You are a smoke test agent. "
            "When asked, call MCP tools exactly as instructed."
        ),
        "description": "MCP e2e smoke",
        "tools": [
            {"type": "agent_toolset_20260401", "default_config": {"enabled": False}}
        ],
        "mcp_servers": [
            {
                "name": "smoke",
                "type": "url",
                "url": mcp_url,
                "authorization_token": "smoke-local-token",
            }
        ],
    }
    print(json.dumps(body))


def cmd_count_agent_versions(_args: list[str]) -> None:
    """Print number of agent versions."""
    data = _load()
    print(len(data.get("data", [])))


def cmd_check_agent(args: list[str]) -> None:
    """Validate agent response: id matches, print name and model."""
    if not args:
        _die("usage: check-agent <agent_id>")
    aid = args[0]
    agent = _load()
    if agent.get("id") != aid:
        _die(f"agent id mismatch: {agent!r}")
    print(f"agent_name={agent.get('name')!r} model={agent.get('model')!r}")


def cmd_check_agents_list(args: list[str]) -> None:
    """Validate that an agent ID appears in a search list response."""
    if not args:
        _die("usage: check-agents-list <agent_id>")
    aid = args[0]
    ids = {row.get("id") for row in _load().get("data", [])}
    if aid not in ids:
        _die(f"agent {aid!r} missing from list q={aid!r}")
    print(f"agents_matched={len(ids)} contains_smoke_agent")


# ─── sessions ───────────────────────────────────────────────────────────────

def cmd_make_session_body(args: list[str]) -> None:
    """Build session POST body JSON."""
    if len(args) < 2:
        _die("usage: make-session-body <agent_id> <env_id> [title]")
    agent_id, env_id = args[0], args[1]
    title = args[2] if len(args) > 2 else "smoke"
    body = {"agent": agent_id, "environment_id": env_id, "title": title}
    print(json.dumps(body))


def cmd_check_session_env(args: list[str]) -> None:
    """Validate session environment_id and print it."""
    if not args:
        _die("usage: check-session-env <expected_env_id>")
    expected = args[0]
    sess = _load()
    actual = sess.get("environment_id")
    if actual != expected:
        _die(f"environment_id mismatch: {actual!r} != {expected!r}")
    print(actual)


def cmd_check_sessions_list(args: list[str]) -> None:
    """Validate that a session ID appears in a search list response."""
    if not args:
        _die("usage: check-sessions-list <session_id>")
    sid = args[0]
    ids = {row.get("id") for row in _load().get("data", [])}
    if sid not in ids:
        _die(f"session {sid!r} missing from list q={sid!r}")
    print(f"sessions_matched={len(ids)} contains_smoke_session")


def cmd_check_files(_args: list[str]) -> None:
    """Validate files response has a data array."""
    resp = _load()
    if "data" not in resp:
        _die(f"missing files.data: {resp!r}")
    files = resp.get("data") or []
    print(f"session_files={len(files)}")


# ─── events shape assertions ────────────────────────────────────────────────

def cmd_assert_events_ama_shape(args: list[str]) -> None:
    """Assert events response has AMA wire shape: data[].{seq, type, ts, data}."""
    label = args[0] if args else "events"
    raw = _load()
    items = raw.get("data")
    if not isinstance(items, list):
        _die(f"{label}: missing data[] list")
    for item in items:
        for key in ("seq", "type", "ts", "data"):
            if key not in item:
                _die(f"{label}: event missing {key!r}: {item!r}")
    print(f"{label}: ama_shape_ok events={len(items)}")


def cmd_assert_trajectory_shape(args: list[str]) -> None:
    """Assert trajectory has correct schema_version and num_events >= min."""
    label = args[0] if args else "trajectory"
    min_events = int(args[1]) if len(args) > 1 else 0
    traj = _load()
    schema = traj.get("schema_version")
    if schema != "oma.trajectory.v1":
        _die(f"{label}: bad schema_version {schema!r}")
    summary = traj.get("summary") or {}
    num_events = int(summary.get("num_events") or 0)
    if num_events < min_events:
        _die(f"{label}: num_events={num_events} want>={min_events}")
    print(f"{label}: trajectory_ok num_events={num_events}")


# ─── events polling checks (exit-code contracts) ────────────────────────────

def cmd_check_events_agent_reply(_args: list[str]) -> None:
    """Check events for a real agent.message with non-empty text.

    Exit codes:
      0 – real text reply found
      1 – evt_fake detected (fake harness mode)
      2 – not ready yet
      3 – session.error received
    """
    events = _load()["data"]
    for evt in events:
        if evt.get("type") == "session.error":
            msg = evt.get("message") or evt.get("error") or "session.error"
            print(msg)
            sys.exit(3)
        if evt.get("type") != "agent.message":
            continue
        if evt.get("id") == "evt_fake":
            sys.exit(1)
        for block in evt.get("content") or []:
            if block.get("type") == "text" and block.get("text", "").strip():
                sys.exit(0)
    sys.exit(2)


def cmd_check_events_bash_uname(_args: list[str]) -> None:
    """Check events for bash tool_use + uname output.

    Exit codes:
      0 – bash used with uname and kernel output found
      1 – evt_fake detected
      2 – not ready yet
      3 – session.error received
      4 – bash workdir missing
    """
    events = _load()["data"]
    bash_use = False
    tool_ok = False
    uname_cmd = re.compile(r"uname\b", re.I)
    kernel_re = re.compile(r"(Linux|Darwin|FreeBSD|OpenBSD|NetBSD|GNU)")

    for evt in events:
        if evt.get("type") == "session.error":
            msg = evt.get("message") or evt.get("error") or "session.error"
            print(msg)
            sys.exit(3)
        if evt.get("id") == "evt_fake":
            sys.exit(1)
        if evt.get("type") == "agent.tool_use" and evt.get("name") == "bash":
            cmd_val = str((evt.get("input") or {}).get("command") or "")
            if uname_cmd.search(cmd_val):
                bash_use = True
        if evt.get("type") != "agent.tool_result":
            continue
        text = ""
        for block in evt.get("content") or []:
            if block.get("type") == "text":
                text += block.get("text", "")
        if "Working directory does not exist" in text:
            print("bash workdir missing — restart platform after SANDBOX_WORKDIR abs fix")
            sys.exit(4)
        if len(text.strip()) >= 16 and kernel_re.search(text):
            tool_ok = True

    if bash_use and tool_ok:
        sys.exit(0)
    sys.exit(2)


def cmd_check_events_mcp_ping(_args: list[str]) -> None:
    """Check events for mcp__smoke__ping tool_use and pong-from-mcp-smoke result.

    Exit codes:
      0 – both tool_use and result found
      2 – not ready yet
      3 – session.error received
    """
    events = _load()["data"]
    saw_tool_use = False
    saw_result = False
    for evt in events:
        if evt.get("type") == "session.error":
            msg = evt.get("message") or evt.get("error") or "session.error"
            print(msg)
            sys.exit(3)
        if evt.get("type") == "agent.tool_use" and evt.get("name") == "mcp__smoke__ping":
            saw_tool_use = True
        if evt.get("type") != "agent.tool_result":
            continue
        for block in evt.get("content") or []:
            text = (block.get("text") or "").strip()
            if "pong-from-mcp-smoke" in text:
                saw_result = True
                break
    if saw_tool_use and saw_result:
        sys.exit(0)
    sys.exit(2)


def cmd_check_events_span_scheduled(args: list[str]) -> None:
    """Check events for span.wakeup_scheduled with matching schedule_id.

    Exit codes:
      0 – found
      2 – not found yet
    """
    if not args:
        _die("usage: check-events-span-scheduled <schedule_id>")
    want = args[0]
    events = _load()["data"]
    for evt in events:
        if evt.get("type") != "span.wakeup_scheduled":
            continue
        if evt.get("schedule_id") == want:
            sys.exit(0)
    sys.exit(2)


def cmd_check_events_wakeup_message(args: list[str]) -> None:
    """Check events for wakeup user.message with matching prompt text.

    Exit codes:
      0 – found
      2 – not found yet
    """
    if not args:
        _die("usage: check-events-wakeup-message <prompt>")
    want = args[0]
    events = _load()["data"]
    for evt in events:
        if evt.get("type") != "user.message":
            continue
        meta = evt.get("metadata") or {}
        if meta.get("harness") != "schedule" or meta.get("kind") != "wakeup":
            continue
        for block in evt.get("content") or []:
            if (block.get("text") or "") == want:
                sys.exit(0)
    sys.exit(2)


# ─── event content extraction ───────────────────────────────────────────────

def cmd_extract_reply_text(_args: list[str]) -> None:
    """Extract first text block from first real agent.message event."""
    events = _load()["data"]
    for evt in events:
        if evt.get("type") != "agent.message" or evt.get("id") == "evt_fake":
            continue
        for block in evt.get("content") or []:
            if block.get("type") == "text":
                print(block.get("text", ""))
                sys.exit(0)
    sys.exit(1)


def cmd_extract_tool_summary(_args: list[str]) -> None:
    """Extract bash command and tool result from events."""
    events = _load()["data"]
    bash_cmd = ""
    tool_text = ""
    for evt in events:
        if evt.get("type") == "agent.tool_use" and evt.get("name") == "bash":
            inp = evt.get("input") or {}
            bash_cmd = str(inp.get("command") or inp.get("cmd") or "")
        if evt.get("type") != "agent.tool_result":
            continue
        for block in evt.get("content") or []:
            if block.get("type") == "text":
                tool_text = block.get("text", "").strip()
                break
    print(f"bash_command={bash_cmd!r}")
    print(f"tool_result={tool_text!r}")


def cmd_check_mcp_events_result(_args: list[str]) -> None:
    """Validate MCP events: find tool_use and pong result, print summary."""
    events = _load()["data"]
    tool_use = ""
    tool_result = ""
    for evt in events:
        if evt.get("type") == "agent.tool_use" and evt.get("name") == "mcp__smoke__ping":
            tool_use = evt.get("name")
        if evt.get("type") == "agent.tool_result":
            for block in evt.get("content") or []:
                if (
                    block.get("type") == "text"
                    and "pong-from-mcp-smoke" in block.get("text", "")
                ):
                    tool_result = block.get("text", "").strip()
    if not tool_use:
        _die("missing agent.tool_use mcp__smoke__ping")
    if not tool_result:
        _die("missing tool_result with pong-from-mcp-smoke")
    print(f"MCP_E2E_OK tool_use={tool_use!r} tool_result={tool_result!r}")


# ─── schedule ───────────────────────────────────────────────────────────────

def cmd_make_schedule_body(args: list[str]) -> None:
    """Build schedule wakeup POST body JSON."""
    if len(args) < 2:
        _die("usage: make-schedule-body <delay_seconds> <prompt>")
    delay_sec = int(args[0])
    prompt = args[1]
    print(json.dumps({"delay_seconds": delay_sec, "prompt": prompt}))


def cmd_check_schedule_list(args: list[str]) -> None:
    """Validate that a schedule ID appears in the wakeups list."""
    if not args:
        _die("usage: check-schedule-list <schedule_id>")
    want = args[0]
    schedules = _load().get("schedules") or []
    if not any(s.get("id") == want for s in schedules):
        _die(f"error: schedule missing from list {schedules!r}")


def cmd_check_schedule_cancel(_args: list[str]) -> None:
    """Validate that a cancel response has cancelled=true."""
    body = _load()
    if body.get("cancelled") is not True:
        _die(f"error: cancel expected true: {body!r}")


def cmd_check_schedule_absent(args: list[str]) -> None:
    """Validate that a schedule ID does NOT appear in the wakeups list."""
    if not args:
        _die("usage: check-schedule-absent <schedule_id>")
    want = args[0]
    schedules = _load().get("schedules") or []
    if any(s.get("id") == want for s in schedules):
        _die(f"error: schedule still listed: {schedules!r}")


def cmd_check_cron_list(args: list[str]) -> None:
    """Validate that a cron schedule is in the list with cron='0 9 * * *'."""
    if not args:
        _die("usage: check-cron-list <cron_id>")
    want = args[0]
    schedules = _load().get("schedules") or []
    row = next((s for s in schedules if s.get("id") == want), None)
    if not row or row.get("cron") != "0 9 * * *":
        _die(f"error: cron row missing {schedules!r}")


# ─── mock MCP helpers ───────────────────────────────────────────────────────

def cmd_check_mock_tools(_args: list[str]) -> None:
    """Validate that mock MCP tools/list response includes a 'ping' tool."""
    r = _load()
    tools = (r.get("result") or {}).get("tools") or []
    if not any(t.get("name") == "ping" for t in tools):
        _die(f"expected 'ping' tool in mock MCP, got: {r!r}")
    print("mock tools/list ok")


def cmd_check_mcp_proxy(_args: list[str]) -> None:
    """Validate MCP proxy initialize response has a 'result' field."""
    r = json.load(open("/tmp/oma-mcp-proxy-smoke.json"))
    if "result" not in r:
        _die(f"mcp-proxy response missing 'result': {r!r}")
    print("mcp-proxy initialize ok")


# ─── runtimes ───────────────────────────────────────────────────────────────

def cmd_check_runtimes_list(args: list[str]) -> None:
    """Validate that a runtime ID appears in the runtimes list."""
    if not args:
        _die("usage: check-runtimes-list <runtime_id>")
    rid = args[0]
    data = _load()
    ids = [r["id"] for r in data.get("runtimes", [])]
    if rid not in ids:
        _die(f"runtime {rid} not in list: {ids!r}")
    print("runtime listed ok")


def cmd_write_bridge_creds(_args: list[str]) -> None:
    """Write bridge credentials JSON to the OMA profile credentials file.

    Reads from environment variables: PLATFORM_URL, RUNTIME_ID, RUNTIME_TOKEN,
    AGENT_API_KEY, MID, CREDS_FILE.
    """
    creds = {
        "v": 2,
        "serverUrl": os.environ["PLATFORM_URL"],
        "runtimeId": os.environ["RUNTIME_ID"],
        "token": os.environ["RUNTIME_TOKEN"],
        "tenants": [
            {
                "id": "default",
                "name": "Default",
                "agentApiKey": os.environ["AGENT_API_KEY"],
            }
        ],
        "machineId": os.environ["MID"],
        "createdAt": int(time.time()),
    }
    path = os.environ["CREDS_FILE"]
    with open(path, "w") as f:
        json.dump(creds, f, indent=2)
    os.chmod(path, 0o600)
    print(path)


def cmd_gen_uuid(_args: list[str]) -> None:
    """Print a new random UUID (for machine-id generation)."""
    print(uuid.uuid4())


def cmd_check_claude_auth(_args: list[str]) -> None:
    """Check claude auth status JSON from stdin; exit 0 if loggedIn, 1 otherwise."""
    d = _load()
    sys.exit(0 if d.get("loggedIn") else 1)


# ─── dreams / cost report ───────────────────────────────────────────────────

def cmd_json_id(_args: list[str]) -> None:
    """Print the 'id' field from stdin JSON (shorthand for json-field id)."""
    data = _load()
    if "id" not in data:
        _die(f"field 'id' not in JSON: {data!r}")
    print(data["id"])


def cmd_json_status(_args: list[str]) -> None:
    """Print the 'status' field from stdin JSON."""
    data = _load()
    print(data.get("status", ""))


def cmd_check_cost_report(_args: list[str]) -> None:
    """Validate cost_report response and print span_count."""
    d = _load()
    if d.get("type") != "cost_report":
        _die(f"expected type=cost_report, got: {d!r}")
    print("cost_report ok", d.get("span_count"))


# ─── dispatch ───────────────────────────────────────────────────────────────

_COMMANDS: dict[str, tuple] = {
    "json-field": (cmd_json_field, "extract a field from stdin JSON"),
    "json-id": (cmd_json_id, "extract 'id' field from stdin JSON"),
    "json-status": (cmd_json_status, "extract 'status' field from stdin JSON"),
    "normalize-events": (cmd_normalize_events, "normalize AMA events envelope"),
    "check-me": (cmd_check_me, "validate /v1/me response"),
    "check-stats": (cmd_check_stats, "validate /v1/stats response"),
    "check-stats-after": (cmd_check_stats_after, "validate stats after session turns"),
    "check-environments": (cmd_check_environments, "validate environments list"),
    "check-environment": (cmd_check_environment, "validate single environment"),
    "check-skills": (cmd_check_skills, "validate skills list"),
    "check-skill-versions": (cmd_check_skill_versions, "validate skill versions >=1"),
    "make-skill-body": (cmd_make_skill_body, "build skill POST body with timestamp"),
    "check-credential": (cmd_check_credential, "validate credential (token redacted)"),
    "count-model-cards": (cmd_count_model_cards, "print count of model cards"),
    "make-model-card-body": (cmd_make_model_card_body, "build model card POST body"),
    "find-model-card": (cmd_find_model_card, "find model card row id by model_id"),
    "make-model-card-update-body": (cmd_make_model_card_update_body, "build model card update body"),
    "check-key-preview": (cmd_check_key_preview, "show API key status"),
    "list-model-card-ids": (cmd_list_model_card_ids, "list all model card IDs"),
    "make-agent-body": (cmd_make_agent_body, "build agent POST body"),
    "make-mcp-agent-body": (cmd_make_mcp_agent_body, "build MCP agent POST body"),
    "count-agent-versions": (cmd_count_agent_versions, "count agent versions"),
    "check-agent": (cmd_check_agent, "validate agent response"),
    "check-agents-list": (cmd_check_agents_list, "validate agent in list"),
    "make-session-body": (cmd_make_session_body, "build session POST body"),
    "check-session-env": (cmd_check_session_env, "validate session environment_id"),
    "check-sessions-list": (cmd_check_sessions_list, "validate session in list"),
    "check-files": (cmd_check_files, "validate files response"),
    "assert-events-ama-shape": (cmd_assert_events_ama_shape, "assert events AMA wire shape"),
    "assert-trajectory-shape": (cmd_assert_trajectory_shape, "assert trajectory schema and event count"),
    "check-events-agent-reply": (cmd_check_events_agent_reply, "check events for real agent.message"),
    "check-events-bash-uname": (cmd_check_events_bash_uname, "check events for bash+uname tool chain"),
    "check-events-mcp-ping": (cmd_check_events_mcp_ping, "check events for mcp__smoke__ping"),
    "check-events-span-scheduled": (cmd_check_events_span_scheduled, "check events for span.wakeup_scheduled"),
    "check-events-wakeup-message": (cmd_check_events_wakeup_message, "check events for wakeup user.message"),
    "extract-reply-text": (cmd_extract_reply_text, "extract first agent.message text"),
    "extract-tool-summary": (cmd_extract_tool_summary, "extract bash cmd and tool result"),
    "check-mcp-events-result": (cmd_check_mcp_events_result, "validate MCP events ping result"),
    "make-schedule-body": (cmd_make_schedule_body, "build schedule wakeup POST body"),
    "check-schedule-list": (cmd_check_schedule_list, "validate schedule in list"),
    "check-schedule-cancel": (cmd_check_schedule_cancel, "validate cancel response"),
    "check-schedule-absent": (cmd_check_schedule_absent, "validate schedule NOT in list"),
    "check-cron-list": (cmd_check_cron_list, "validate cron in list"),
    "check-mock-tools": (cmd_check_mock_tools, "validate mock MCP tools/list"),
    "check-mcp-proxy": (cmd_check_mcp_proxy, "validate MCP proxy initialize response"),
    "check-runtimes-list": (cmd_check_runtimes_list, "validate runtime in list"),
    "write-bridge-creds": (cmd_write_bridge_creds, "write bridge credentials file"),
    "gen-uuid": (cmd_gen_uuid, "print a new random UUID"),
    "check-claude-auth": (cmd_check_claude_auth, "check claude auth status (loggedIn)"),
    "json-id": (cmd_json_id, "extract 'id' from stdin JSON"),
    "json-status": (cmd_json_status, "extract 'status' from stdin JSON"),
    "check-cost-report": (cmd_check_cost_report, "validate cost_report response"),
}


def main() -> None:
    if len(sys.argv) < 2 or sys.argv[1] in ("-h", "--help"):
        print("Usage: smoke_utils.py <command> [args...]")
        print()
        print("Commands:")
        seen: set[str] = set()
        for name, (_, desc) in _COMMANDS.items():
            if name not in seen:
                print(f"  {name:<36} {desc}")
                seen.add(name)
        sys.exit(0)

    cmd_name = sys.argv[1]
    cmd_args = sys.argv[2:]

    if cmd_name not in _COMMANDS:
        _die(f"unknown command: {cmd_name!r}. Run with --help to see available commands.")

    fn, _ = _COMMANDS[cmd_name]
    fn(cmd_args)


if __name__ == "__main__":
    main()
