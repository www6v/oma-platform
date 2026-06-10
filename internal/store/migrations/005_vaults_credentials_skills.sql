CREATE TABLE vaults (
  id TEXT PRIMARY KEY NOT NULL,
  tenant_id TEXT NOT NULL,
  name TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER,
  archived_at INTEGER
);

CREATE INDEX idx_vaults_tenant ON vaults (tenant_id, archived_at);
CREATE INDEX idx_vaults_tenant_created_id ON vaults (tenant_id, created_at, id);

CREATE TABLE credentials (
  id TEXT PRIMARY KEY NOT NULL,
  tenant_id TEXT NOT NULL,
  vault_id TEXT NOT NULL,
  display_name TEXT NOT NULL,
  auth_type TEXT NOT NULL,
  mcp_server_url TEXT,
  provider TEXT,
  auth_cipher TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER,
  archived_at INTEGER
);

CREATE INDEX idx_credentials_vault ON credentials (tenant_id, vault_id, archived_at);
CREATE UNIQUE INDEX idx_credentials_mcp_url_active ON credentials (
  tenant_id, vault_id, mcp_server_url
) WHERE mcp_server_url IS NOT NULL AND archived_at IS NULL;
CREATE INDEX idx_credentials_provider ON credentials (
  tenant_id, vault_id, provider
) WHERE provider IS NOT NULL;

CREATE TABLE skills (
  id TEXT PRIMARY KEY NOT NULL,
  tenant_id TEXT NOT NULL,
  display_title TEXT NOT NULL,
  name TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  source TEXT NOT NULL DEFAULT 'custom',
  latest_version TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER
);

CREATE INDEX idx_skills_tenant ON skills (tenant_id, created_at);

CREATE TABLE skill_versions (
  skill_id TEXT NOT NULL,
  tenant_id TEXT NOT NULL,
  version TEXT NOT NULL,
  files_json TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (skill_id, version)
);

CREATE INDEX idx_skill_versions_tenant ON skill_versions (tenant_id, skill_id);
