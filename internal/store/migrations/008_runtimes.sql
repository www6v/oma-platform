-- Local ACP runtime registration (AMA wire-compatible).

CREATE TABLE IF NOT EXISTS runtimes (
  id                TEXT PRIMARY KEY NOT NULL,
  owner_user_id     TEXT NOT NULL,
  owner_tenant_id   TEXT NOT NULL,
  machine_id        TEXT NOT NULL,
  hostname          TEXT NOT NULL,
  os                TEXT NOT NULL,
  agents_json       TEXT NOT NULL DEFAULT '[]',
  local_skills_json TEXT NOT NULL DEFAULT '{}',
  version           TEXT NOT NULL,
  status            TEXT NOT NULL DEFAULT 'offline',
  last_heartbeat    INTEGER,
  created_at        INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_runtimes_user_machine
  ON runtimes (owner_user_id, machine_id);

CREATE INDEX IF NOT EXISTS idx_runtimes_tenant
  ON runtimes (owner_tenant_id, created_at DESC);

CREATE TABLE IF NOT EXISTS runtime_tokens (
  id                  TEXT PRIMARY KEY NOT NULL,
  runtime_id          TEXT NOT NULL,
  token_hash          TEXT NOT NULL UNIQUE,
  created_by_user_id  TEXT NOT NULL,
  revoked_at          INTEGER,
  last_used_at        INTEGER,
  created_at          INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_runtime_tokens_runtime
  ON runtime_tokens (runtime_id, revoked_at);

CREATE TABLE IF NOT EXISTS connect_runtime_codes (
  code        TEXT PRIMARY KEY NOT NULL,
  user_id     TEXT NOT NULL,
  tenant_id   TEXT NOT NULL,
  state       TEXT NOT NULL,
  expires_at  INTEGER NOT NULL,
  used_at     INTEGER
);

CREATE INDEX IF NOT EXISTS idx_connect_runtime_codes_expires
  ON connect_runtime_codes (expires_at);

CREATE TABLE IF NOT EXISTS runtime_tenants (
  runtime_id        TEXT NOT NULL,
  tenant_id         TEXT NOT NULL,
  agent_api_key_id  TEXT NOT NULL,
  created_at        INTEGER NOT NULL,
  revoked_at        INTEGER,
  PRIMARY KEY (runtime_id, tenant_id)
);

CREATE INDEX IF NOT EXISTS idx_runtime_tenants_runtime
  ON runtime_tenants (runtime_id, revoked_at);

CREATE INDEX IF NOT EXISTS idx_runtime_tenants_tenant
  ON runtime_tenants (tenant_id, revoked_at);
