CREATE TABLE IF NOT EXISTS dreams (
  id                       TEXT PRIMARY KEY NOT NULL,
  tenant_id                TEXT NOT NULL,
  status                   TEXT NOT NULL,
  input_memory_store_id    TEXT NOT NULL,
  input_session_ids        TEXT NOT NULL,
  output_memory_store_id   TEXT,
  model                    TEXT NOT NULL,
  instructions             TEXT,
  session_id               TEXT,
  usage                    TEXT NOT NULL,
  error                    TEXT,
  created_at               INTEGER NOT NULL,
  started_at               INTEGER,
  ended_at                 INTEGER,
  archived_at              INTEGER
);

CREATE INDEX IF NOT EXISTS idx_dreams_tenant_created
  ON dreams (tenant_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_dreams_input_store
  ON dreams (input_memory_store_id, status);

CREATE INDEX IF NOT EXISTS idx_dreams_output_store
  ON dreams (output_memory_store_id, status)
  WHERE output_memory_store_id IS NOT NULL;
