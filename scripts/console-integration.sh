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


def check(name, cond, detail=""):
    if not cond:
        failures.append(f"{name}: {detail}")


print(f"==> Console API integration against {API}")

_, agents = req("GET", "/v1/agents?limit=20&status=active")
check("agents_has_more", "has_more" in agents, list(agents.keys()))
check("agents_data_array", isinstance(agents.get("data"), list))

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

_, detail = req("GET", f"/v1/agents/{aid}")
check("get_agent", detail.get("id") == aid, detail)
check(
    "get_mcp_servers",
    isinstance(detail.get("mcp_servers"), list),
    detail.get("mcp_servers"),
)

_, updated = req(
    "POST",
    f"/v1/agents/{aid}",
    {"description": "updated via POST"},
)
check(
    "post_update",
    updated.get("description") == "updated via POST",
    updated.get("description"),
)

_, versions = req("GET", f"/v1/agents/{aid}/versions")
check("versions_data", isinstance(versions.get("data"), list), versions)
if versions.get("data"):
    snap = versions["data"][0]
    check("version_ama_shape", snap.get("type") == "agent", snap)
    check("version_no_wrapper", "snapshot" not in snap, snap)

for path in (
    "/v1/agents?limit=200&status=any",
    "/v1/skills",
    "/v1/model_cards?limit=200",
    "/v1/runtimes",
    "/v1/models/list",
    "/v1/environments?limit=5",
    "/v1/stats",
    "/v1/me",
    "/v1/files",
    "/v1/memory_stores",
    "/v1/evals/runs",
    "/v1/integrations/linear/installations",
):
    req("GET", path)

_, envs = req("GET", "/v1/environments?limit=5")
env_id = envs["data"][0]["id"]
_, sess = req(
    "POST",
    "/v1/sessions",
    {"agent": aid, "environment_id": env_id},
    expect=(201,),
)
sid = sess.get("id")
check("create_session", sid, sess)

for suffix in ("threads", "pending", "trajectory", "outputs"):
    req("GET", f"/v1/sessions/{sid}/{suffix}")

if sid:
    req("DELETE", f"/v1/sessions/{sid}", expect=(200,))
if aid:
    req("DELETE", f"/v1/agents/{aid}", expect=(200,))

if failures:
    print("Console API integration FAILED:", file=sys.stderr)
    for item in failures:
        print(f"  - {item}", file=sys.stderr)
    sys.exit(1)

print("Console API integration: OK")
PY

if [[ -x "$(command -v node)" ]] && [[ -f "${ROOT_DIR}/scripts/console-ui-check.mjs" ]]; then
  if node -e "import('playwright')" >/dev/null 2>&1; then
    echo "==> Console UI smoke (Playwright headless)"
    node "${ROOT_DIR}/scripts/console-ui-check.mjs"
  else
    echo "==> skip UI check (npm install playwright && npx playwright install chromium)"
  fi
fi
