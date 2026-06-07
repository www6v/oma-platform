CREATE TABLE environments (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL DEFAULT 'default',
  name TEXT NOT NULL,
  description TEXT,
  status TEXT NOT NULL DEFAULT 'ready',
  config TEXT NOT NULL,
  metadata TEXT,
  created_at INTEGER NOT NULL,
  updated_at INTEGER,
  archived_at INTEGER
);

CREATE INDEX idx_environments_tenant ON environments(tenant_id);

CREATE TABLE model_cards (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL DEFAULT 'default',
  model_id TEXT NOT NULL,
  model TEXT NOT NULL,
  provider TEXT NOT NULL,
  base_url TEXT,
  custom_headers TEXT,
  api_key_cipher TEXT NOT NULL,
  api_key_preview TEXT NOT NULL,
  is_default INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER,
  archived_at INTEGER,
  UNIQUE (tenant_id, model_id)
);

CREATE UNIQUE INDEX idx_model_cards_default
  ON model_cards(tenant_id)
  WHERE is_default = 1 AND archived_at IS NULL;

CREATE INDEX idx_model_cards_tenant ON model_cards(tenant_id);

ALTER TABLE sessions ADD COLUMN environment_id TEXT NOT NULL DEFAULT '';
ALTER TABLE sessions ADD COLUMN environment_snapshot TEXT NOT NULL DEFAULT '{}';
