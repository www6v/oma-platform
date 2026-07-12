-- Platform-managed per-tenant daemon pool (pluggable-harness §10).
-- Phase 3: schema only — rows are written by the Phase 4 pool manager.

CREATE TABLE IF NOT EXISTS system_runtimes (
  id                TEXT PRIMARY KEY NOT NULL,
  tenant_id         TEXT    NOT NULL,
  agent             TEXT    NOT NULL,
  status            TEXT    NOT NULL DEFAULT 'warming',
  sandbox_id        TEXT,
  created_at        INTEGER NOT NULL,
  last_heartbeat_at INTEGER,
  idle_since_at     INTEGER,
  stopped_at        INTEGER
);

CREATE INDEX IF NOT EXISTS idx_system_runtimes_tenant_agent
  ON system_runtimes (tenant_id, agent, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_system_runtimes_status
  ON system_runtimes (status, idle_since_at);

CREATE TABLE IF NOT EXISTS system_runtime_leases (
  id                TEXT PRIMARY KEY NOT NULL,
  system_runtime_id TEXT NOT NULL,
  tenant_id         TEXT NOT NULL,
  session_id        TEXT NOT NULL,
  acquired_at       INTEGER NOT NULL,
  released_at       INTEGER,
  idle_ttl_at       INTEGER,
  FOREIGN KEY (system_runtime_id) REFERENCES system_runtimes (id)
);

CREATE INDEX IF NOT EXISTS idx_system_runtime_leases_runtime
  ON system_runtime_leases (system_runtime_id, released_at);

CREATE INDEX IF NOT EXISTS idx_system_runtime_leases_session
  ON system_runtime_leases (session_id, released_at);

CREATE INDEX IF NOT EXISTS idx_system_runtime_leases_ttl
  ON system_runtime_leases (idle_ttl_at) WHERE released_at IS NOT NULL;
