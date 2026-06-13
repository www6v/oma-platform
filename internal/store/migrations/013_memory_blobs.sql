-- Memory blob offload + version retention index.

ALTER TABLE memories ADD COLUMN blob_key TEXT;

CREATE INDEX IF NOT EXISTS idx_memory_versions_created
  ON memory_versions (created_at);
