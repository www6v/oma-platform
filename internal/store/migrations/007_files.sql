CREATE TABLE files (
  id TEXT PRIMARY KEY NOT NULL,
  tenant_id TEXT NOT NULL,
  session_id TEXT,
  scope TEXT NOT NULL,
  filename TEXT NOT NULL,
  media_type TEXT NOT NULL,
  size_bytes INTEGER NOT NULL,
  downloadable INTEGER NOT NULL DEFAULT 0,
  blob_key TEXT NOT NULL,
  created_at INTEGER NOT NULL
);

CREATE INDEX idx_files_tenant ON files (tenant_id, created_at, id);
CREATE INDEX idx_files_session ON files (session_id, created_at, id);
