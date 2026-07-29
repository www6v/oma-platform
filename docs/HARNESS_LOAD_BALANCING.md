# Harness load balancing (multi-piPy)

Same-host piPy replicas behind a **dedicated** Nginx LB (`oma-harness-lb`).
The Go platform keeps a single `HARNESS_URL` pointing at the LB. Session
affinity uses header `X-OMA-Session-ID` (consistent hash).

An existing host Nginx on **:80** (e.g. `notebooklm-nginx`) is unrelated and
does **not** conflict with the harness LB on **:8090**.

## Topology

```
Console → oma-platform (Go :8787)
              │  HARNESS_URL=http://oma-harness-lb:8090
              ▼
         oma-harness-lb (Nginx :8090)
              │  hash $http_x_oma_session_id consistent
     ┌────────┴────────┐
     ▼                 ▼
oma-harness-1     oma-harness-2
     │                 │
     └────────┬────────┘
              ▼
      shared /data volume
```

Remote piPy hosts are **cold standby** in this phase (no shared NFS). Cut over
by changing `HARNESS_URL` and restarting the platform — see below.

Related design: [pipy-litellm-proxy-design.md](./design/pipy-litellm-proxy-design.md)
Phase 2 (multi-piPy) is partially realized here via external LB. LiteLLM
virtual keys remain per-replica configuration (not provisioned by OMA).

## Prerequisites

1. **MySQL `DATABASE_URL`** — required for multi-replica. Team / workflow state
   must not use a shared `OMA_DATABASE_PATH` SQLite file across processes.
2. **Shared `/data`** — platform and all same-host harness replicas mount the
   same volume (`../data:/data` in compose) so `workdir` / sandboxes resolve.
3. **Sticky header** — Go sets `X-OMA-Session-ID` on turn and workflow-proxy
   requests. Do not strip this header at the LB.

## Sticky header contract

| Traffic | Header value |
|---------|----------------|
| `POST /internal/turn` / `/internal/turn/stream` | `TurnRequest.session_id` |
| `/api/workflows/{id}…` | workflow id |
| `/api/workflows/executions/{id}…` | execution id |
| List / generate / health / validate / templates | unset (hashes to one stable peer) |

Nginx upstream:

```nginx
hash $http_x_oma_session_id consistent;
```

Streaming: `/internal/turn/stream` uses `proxy_buffering off` and 900s
read/send timeouts.

## Deploy (docker compose)

From `oma-platform/deploy/`:

```bash
./docker.sh up
# or:
docker compose --env-file ../.env -f docker-compose.yml up -d --build --remove-orphans
```

### Port 8090 already allocated

`oma-harness-lb` publishes host `:8090`. After migrating from a single
`oma-harness` container, an **orphan** often still holds that port:

```text
Bind for 0.0.0.0:8090 failed: port is already allocated
Found orphan containers ([deploy-oma-harness-1]) ...
```

Fix on the host:

```bash
docker ps -a --format '{{.Names}}\t{{.Ports}}' | grep 8090
docker rm -f deploy-oma-harness-1   # or whatever still maps 8090
./docker.sh up                      # uses --remove-orphans
```

Services:

| Service | Role |
|---------|------|
| `oma-harness-1` / `oma-harness-2` | piPy replicas (no host port) |
| `oma-harness-lb` | Nginx on host `:8090` |
| `oma-platform` | `HARNESS_URL=http://oma-harness-lb:8090` |

Host `:80` Nginx (notebooklm, etc.) can keep running unchanged.

### Per-replica LiteLLM virtual keys

Shared `~/.pi/agent` is mounted read-only into every replica. To isolate
quota with distinct virtual keys:

```bash
export HARNESS_LITELLM_KEY_1=sk-...
export HARNESS_LITELLM_KEY_2=sk-...
docker compose -f deploy/docker-compose.yml \
  -f deploy/docker-compose.harness-keys.yml up -d
```

The override injects `OPENAI_API_KEY` / `LITELLM_API_KEY` per replica.

### Add a third replica

1. Copy `oma-harness-2` to `oma-harness-3` in `deploy/docker-compose.yml`.
2. Add `server oma-harness-3:8090 ...;` to `upstream oma_harness` in
   `deploy/nginx-harness.conf`.
3. Add `depends_on` / health for the new replica on platform and lb.
4. Recreate lb + new replica. Prefer low traffic: consistent-hash peer set
   changes remaps some sessions.

### Single replica / local dev without LB

Point `HARNESS_URL` at one harness (`http://127.0.0.1:8090` via
`./start-harness.sh`). Sticky header is harmless when only one backend exists.

## Verify sticky routing

```bash
# Same session id should hit the same upstream (check lb access/error logs
# or add a temporary debug header on each harness).
curl -s -D- -o /dev/null \
  -H "Content-Type: application/json" \
  -H "X-OMA-Session-ID: sess-sticky-a" \
  -d '{"session_id":"sess-sticky-a","agent":{"id":"a","name":"a","model":"x","version":1},"events":[],"workdir":"/tmp"}' \
  http://127.0.0.1:8090/internal/turn
```

Repeat with a different `X-OMA-Session-ID`; over many ids both replicas
should receive traffic.

## Remote cold standby

1. Deploy piPy on the remote host with its own LiteLLM virtual key and local
   disk (no shared `/data` with the primary host).
2. To fail over: set platform `HARNESS_URL=http://<remote>:8090` (bypass lb
   or retarget lb upstream to the remote only) and restart `oma-platform`.
3. **Limitation:** sessions whose `workdir` lived on the primary `/data` will
   not see files on the remote. Prefer standby for new sessions or after
   copying needed artifacts.

There is **no** automatic failover in this phase.

## Failure notes

| Case | Behavior |
|------|----------|
| One replica unhealthy | Nginx `max_fails` / compose healthcheck; consistent hash remaps some sessions |
| Mid-stream replica death | Turn fails; retry may land on another peer; in-process teammate loop on the dead peer is lost |
| Scale up/down | Prefer quiet periods; hash ring change remaps a fraction of sessions |

## Files

| Path | Purpose |
|------|---------|
| `deploy/nginx-harness.conf` | LB config for `oma-harness-lb` |
| `deploy/docker-compose.yml` | harness-1/2 + lb |
| `deploy/docker-compose.harness-keys.yml` | optional per-replica keys |
| `internal/harness/client.go` | `X-OMA-Session-ID` on turns |
| `internal/api/workflows_proxy.go` | sticky id from workflow paths |
