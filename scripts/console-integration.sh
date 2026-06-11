#!/usr/bin/env bash
# Console API integration — validates wire shapes the Console SPA expects.
# Requires oma-server on ${OMA_BASE:-http://127.0.0.1:8787} with ${OMA_API_KEY:-dev-key}.
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
OMA_BASE="${OMA_BASE:-http://127.0.0.1:8787}"
OMA_API_KEY="${OMA_API_KEY:-dev-key}"

export OMA_BASE OMA_API_KEY

python3 <<'PY'
import json
import os
import sys
import urllib.error
import urllib.request

API = os.environ["OMA_BASE"].rstrip("/")
KEY = os.environ["OMA_API_KEY"]
failures = []
aid = None
sid = None
fid = None


def log(msg):
    print(f"  [console-integration] {msg}", flush=True)


def req(method, path, body=None, expect=(200, 201)):
    data = None
    headers = {
        "Authorization": f"Bearer {KEY}",
        "Content-Type": "application/json",
    }
    if body is not None:
        data = json.dumps(body).encode()
    request = urllib.request.Request(
        API + path, data=data, headers=headers, method=method,
    )
    try:
        with urllib.request.urlopen(request) as resp:
            status = resp.status
            raw = resp.read()
            parsed = json.loads(raw) if raw else {}
            if status not in expect:
                failures.append(
                    f"{method} {path}: status {status} expected {expect}",
                )
            return status, parsed
    except urllib.error.HTTPError as err:
        raw = err.read()
        try:
            parsed = json.loads(raw)
        except Exception:
            parsed = raw.decode(errors="replace")
        failures.append(f"{method} {path}: HTTP {err.code} {parsed}")
        return err.code, parsed


def req_text(method, path, expect=(200,)):
    headers = {"Authorization": f"Bearer {KEY}"}
    request = urllib.request.Request(
        API + path, headers=headers, method=method,
    )
    try:
        with urllib.request.urlopen(request) as resp:
            status = resp.status
            raw = resp.read().decode()
            if status not in expect:
                failures.append(
                    f"{method} {path}: status {status} expected {expect}",
                )
            return status, raw
    except urllib.error.HTTPError as err:
        raw = err.read().decode(errors="replace")
        failures.append(f"{method} {path}: HTTP {err.code} {raw}")
        return err.code, raw


def check(name, cond, detail=""):
    if not cond:
        failures.append(f"{name}: {detail}")


print(f"==> Console API integration against {API}")
log("listing active agents (GET /v1/agents?limit=20&status=active)")

_, agents = req("GET", "/v1/agents?limit=20&status=active")
check("agents_has_more", "has_more" in agents, list(agents.keys()))
check("agents_data_array", isinstance(agents.get("data"), list))
log(f"agents list OK — count={len(agents.get('data', []))}, has_more={agents.get('has_more')}")

log("creating test agent (POST /v1/agents) name=console-integration")
_, created = req(
    "POST",
    "/v1/agents",
    {
        "name": "console-integration",
        "model": {"id": "claude-sonnet-4-6", "speed": "standard"},
        "system": "Console integration test agent.",
        "description": "console-integration.sh",
        "mcp_servers": [
            {"name": "demo", "type": "url", "url": "http://localhost/mcp"},
        ],
        "skills": [{"type": "anthropic", "skill_id": "builtin_pdf"}],
    },
    expect=(201,),
)
aid = created.get("id") if isinstance(created, dict) else None
log(f"agent created — id={aid}, model={created.get('model', {}).get('id')}")
check("create_agent", aid, created)
check("create_type", created.get("type") == "agent", created)
check(
    "create_model_object",
    isinstance(created.get("model"), dict)
    and created["model"].get("id"),
    created.get("model"),
)
check(
    "create_created_at_iso",
    isinstance(created.get("created_at"), str),
    created.get("created_at"),
)

log(f"fetching agent detail (GET /v1/agents/{aid})")
_, detail = req("GET", f"/v1/agents/{aid}")
check("get_agent", detail.get("id") == aid, detail)
log(f"agent detail OK — mcp_servers={len(detail.get('mcp_servers', []))}")
check(
    "get_mcp_servers",
    isinstance(detail.get("mcp_servers"), list),
    detail.get("mcp_servers"),
)

log(f"updating agent description (POST /v1/agents/{aid})")
_, updated = req(
    "POST",
    f"/v1/agents/{aid}",
    {"description": "updated via POST"},
)
log(f"agent updated — description={updated.get('description')!r}")
check(
    "post_update",
    updated.get("description") == "updated via POST",
    updated.get("description"),
)

log(f"listing agent versions (GET /v1/agents/{aid}/versions)")
_, versions = req("GET", f"/v1/agents/{aid}/versions")
check("versions_data", isinstance(versions.get("data"), list), versions)
log(f"agent versions OK — count={len(versions.get('data', []))}")
if versions.get("data"):
    snap = versions["data"][0]
    check("version_ama_shape", snap.get("type") == "agent", snap)
    check("version_no_wrapper", "snapshot" not in snap, snap)

console_endpoints = (
    ("/v1/agents?limit=200&status=any", "all agents"),
    ("/v1/skills", "skills catalog"),
    ("/v1/model_cards?limit=200", "model cards"),
    ("/v1/runtimes", "runtimes"),
    ("/v1/models/list", "models list"),
    ("/v1/environments?limit=5", "environments"),
    ("/v1/stats", "stats"),
    ("/v1/me", "current user"),
    ("/v1/files", "files list"),
    ("/v1/memory_stores", "memory stores"),
    ("/v1/evals/runs", "eval runs"),
    ("/v1/integrations/linear/installations", "linear installations"),
)
for path, label in console_endpoints:
    log(f"probing console endpoint — {label} (GET {path})")
    req("GET", path)

log("creating session (POST /v1/sessions) for integration agent")
_, envs = req("GET", "/v1/environments?limit=5")
env_id = envs["data"][0]["id"]
log(f"using environment — id={env_id}")
_, sess = req(
    "POST",
    "/v1/sessions",
    {"agent": aid, "environment_id": env_id},
    expect=(201,),
)
sid = sess.get("id")
log(f"session created — id={sid}, agent={sess.get('agent', {}).get('id')}")
check("create_session", sid, sess)
check(
    "session_agent_object",
    isinstance(sess.get("agent"), dict) and sess["agent"].get("id"),
    sess.get("agent"),
)
check("session_type", sess.get("type") == "session", sess.get("type"))

log("connect-runtime flow (POST /v1/runtimes/connect-runtime + exchange)")
runtime_state = "integration-runtime-state"
_, connect = req(
    "POST",
    "/v1/runtimes/connect-runtime",
    {"state": runtime_state},
    expect=(200,),
)
check("connect_runtime_code", isinstance(connect.get("code"), str) and connect["code"], connect)
check("connect_runtime_expires", isinstance(connect.get("expires_at"), int), connect)
_, exchanged = req(
    "POST",
    "/agents/runtime/exchange",
    {
        "code": connect["code"],
        "state": runtime_state,
        "machine_id": "integration-machine",
        "hostname": "integration-host",
        "os": "darwin",
        "version": "0.0.1-integration",
    },
    expect=(200,),
)
check(
    "runtime_exchange_id",
    isinstance(exchanged.get("runtime_id"), str) and exchanged["runtime_id"],
    exchanged,
)
check(
    "runtime_exchange_token",
    isinstance(exchanged.get("token"), str)
    and exchanged["token"].startswith("sk_machine_"),
    exchanged,
)
check(
    "runtime_exchange_agent_key",
    isinstance(exchanged.get("agent_api_key"), str)
    and exchanged["agent_api_key"].startswith("oma_"),
    exchanged,
)
runtime_id = exchanged.get("runtime_id")
_, runtime_list = req("GET", "/v1/runtimes")
check(
    "runtime_list_nonempty",
    isinstance(runtime_list.get("runtimes"), list)
    and len(runtime_list["runtimes"]) >= 1,
    runtime_list,
)
if runtime_id:
    req("DELETE", f"/v1/runtimes/{runtime_id}", expect=(200,))

log("linear publication-first flow (POST/PATCH /v1/integrations/linear/...)")
_, linear_pub = req(
    "POST",
    "/v1/integrations/linear/publications",
    {
        "agentId": aid,
        "environmentId": env_id,
        "personaName": "Console Integration Bot",
        "returnUrl": "http://localhost/console/integrations",
    },
    expect=(200,),
)
check(
    "linear_publication_id",
    isinstance(linear_pub.get("publication_id"), str)
    and linear_pub["publication_id"],
    linear_pub,
)
pub_id = linear_pub.get("publication_id")
_, pending_pubs = req(
    "GET",
    "/v1/integrations/linear/publications?status=pending",
    expect=(200,),
)
check(
    "linear_pending_publications",
    isinstance(pending_pubs.get("data"), list)
    and len(pending_pubs["data"]) >= 1,
    pending_pubs,
)
if pub_id:
    _, creds = req(
        "PATCH",
        f"/v1/integrations/linear/publications/{pub_id}/credentials",
        {
            "clientId": "console-integration-client",
            "clientSecret": "console-integration-secret",
            "webhookSecret": "lin_wh_console_integration",
            "returnUrl": "http://localhost/console/integrations",
        },
        expect=(200,),
    )
    check(
        "linear_install_url",
        isinstance(creds.get("install_url"), str) and creds["install_url"],
        creds,
    )
    _, rule = req(
        "POST",
        f"/v1/integrations/linear/publications/{pub_id}/dispatch-rules",
        {"filter_label": "bot-ready", "name": "Integration pickup"},
        expect=(201,),
    )
    check("linear_dispatch_rule_id", isinstance(rule.get("id"), str), rule)
    _, rules = req(
        "GET",
        f"/v1/integrations/linear/publications/{pub_id}/dispatch-rules",
        expect=(200,),
    )
    check(
        "linear_dispatch_rules",
        isinstance(rules.get("rules"), list) and len(rules["rules"]) >= 1,
        rules,
    )
    req(
        "DELETE",
        f"/v1/integrations/linear/publications/{pub_id}",
        expect=(200,),
    )

fid = None
if sid:
    log(f"uploading file to session scope (POST /v1/files) scope_id={sid}")
    _, uploaded = req(
        "POST",
        "/v1/files",
        {
            "filename": "integration.txt",
            "content": "console integration upload",
            "media_type": "text/plain",
            "scope_id": sid,
            "downloadable": True,
        },
        expect=(201,),
    )
    check("file_upload_id", uploaded.get("id", "").startswith("file-"), uploaded)
    check("file_upload_scope", uploaded.get("scope_id") == sid, uploaded)
    fid = uploaded.get("id")
    log(f"file uploaded — id={fid}, filename=integration.txt")
    if fid:
        log(f"downloading file content (GET /v1/files/{fid}/content)")
        _, content = req_text("GET", f"/v1/files/{fid}/content", expect=(200,))
        check(
            "file_download",
            isinstance(content, str) and "integration upload" in content,
            content,
        )
        log(f"file download OK — bytes={len(content)}")

for suffix in ("threads", "pending", "trajectory", "outputs"):
    log(f"fetching session sub-resource (GET /v1/sessions/{sid}/{suffix})")
    req("GET", f"/v1/sessions/{sid}/{suffix}")

log("skipping cleanup — agent/session/file left for manual frontend verification")

if failures:
    print("Console API integration FAILED:", file=sys.stderr)
    for item in failures:
        print(f"  - {item}", file=sys.stderr)
    sys.exit(1)

print("Console API integration: OK")
print("")
print("==> Resources kept for manual verification:")
print(f"    Agent:   {aid}")
print(f"    Session: {sid}")
print(f"    File:    {fid or '(none)'}")
print(f"    Console: open agent/session in UI using the IDs above")
PY

if [[ -x "$(command -v node)" ]] && [[ -f "${ROOT_DIR}/scripts/console-ui-check.mjs" ]]; then
  if node -e "import('playwright')" >/dev/null 2>&1; then
    echo "==> Console UI smoke (Playwright headless)"
    node "${ROOT_DIR}/scripts/console-ui-check.mjs"
    if [[ -f "${ROOT_DIR}/scripts/console-dogfood.mjs" ]]; then
      echo "==> Console UI dogfood (create agent + session + message)"
      node "${ROOT_DIR}/scripts/console-dogfood.mjs"
    fi
  else
    echo "==> skip UI check (npm install playwright in scripts/ && npx playwright install chromium)"
  fi
fi
