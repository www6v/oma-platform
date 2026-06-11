-- Memory stores + eval runs — Console / AMA wire-compatible MVP (inline content).

CREATE TABLE IF NOT EXISTS memory_stores (
  id            TEXT PRIMARY KEY NOT NULL,
  tenant_id     TEXT NOT NULL,
  name          TEXT NOT NULL,
  description   TEXT,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER,
  archived_at   INTEGER
);

CREATE INDEX IF NOT EXISTS idx_memory_stores_tenant
  ON memory_stores (tenant_id, created_at);

CREATE TABLE IF NOT EXISTS memories (
  id              TEXT PRIMARY KEY NOT NULL,
  store_id        TEXT NOT NULL,
  path            TEXT NOT NULL,
  content         TEXT NOT NULL DEFAULT '',
  content_sha256  TEXT NOT NULL,
  etag            TEXT NOT NULL,
  size_bytes      INTEGER NOT NULL,
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL,
  FOREIGN KEY (store_id) REFERENCES memory_stores(id) ON DELETE CASCADE,
  UNIQUE (store_id, path)
);

CREATE INDEX IF NOT EXISTS idx_memories_store_updated
  ON memories (store_id, updated_at);

CREATE TABLE IF NOT EXISTS memory_versions (
  id              TEXT PRIMARY KEY NOT NULL,
  memory_id       TEXT NOT NULL,
  store_id        TEXT NOT NULL,
  operation       TEXT NOT NULL,
  path            TEXT,
  content         TEXT,
  content_sha256  TEXT,
  size_bytes      INTEGER,
  actor_type      TEXT NOT NULL,
  actor_id        TEXT NOT NULL,
  created_at      INTEGER NOT NULL,
  redacted        INTEGER NOT NULL DEFAULT 0,
  FOREIGN KEY (store_id) REFERENCES memory_stores(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_memory_versions_memory
  ON memory_versions (memory_id, created_at);

CREATE INDEX IF NOT EXISTS idx_memory_versions_store
  ON memory_versions (store_id, created_at);

CREATE TABLE IF NOT EXISTS eval_runs (
  id               TEXT PRIMARY KEY NOT NULL,
  tenant_id        TEXT NOT NULL,
  agent_id         TEXT NOT NULL,
  environment_id   TEXT NOT NULL,
  suite            TEXT,
  status           TEXT NOT NULL,
  started_at       INTEGER NOT NULL,
  completed_at     INTEGER,
  results          TEXT,
  score            REAL,
  error            TEXT
);

CREATE INDEX IF NOT EXISTS idx_eval_runs_tenant_started
  ON eval_runs (tenant_id, started_at);

CREATE INDEX IF NOT EXISTS idx_eval_runs_tenant_agent_started
  ON eval_runs (tenant_id, agent_id, started_at);

CREATE INDEX IF NOT EXISTS idx_eval_runs_tenant_environment_started
  ON eval_runs (tenant_id, environment_id, started_at);

CREATE INDEX IF NOT EXISTS idx_eval_runs_status_active
  ON eval_runs (status, started_at);
