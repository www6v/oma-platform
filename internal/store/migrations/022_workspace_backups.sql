CREATE TABLE workspace_backups (
  id                INTEGER PRIMARY KEY AUTOINCREMENT,
  tenant_id         TEXT    NOT NULL,
  environment_id    TEXT    NOT NULL,
  backup_handle     TEXT    NOT NULL,
  created_at        INTEGER NOT NULL,
  expires_at        INTEGER NOT NULL,
  source_session_id TEXT
);

CREATE INDEX idx_workspace_backups_scope_recent
  ON workspace_backups (tenant_id, environment_id, created_at DESC);

CREATE INDEX idx_workspace_backups_expires
  ON workspace_backups (expires_at);

CREATE INDEX idx_workspace_backups_session
  ON workspace_backups (source_session_id, created_at DESC);
