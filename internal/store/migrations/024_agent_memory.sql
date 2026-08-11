-- Agent built-in memory (Hermes parity): store kind discriminator.
-- kind='agent_builtin' rows are per-agent MEMORY.md/USER.md stores managed by
-- the harness memory extension; they are hidden from default store listings.

ALTER TABLE memory_stores ADD COLUMN kind TEXT NOT NULL DEFAULT 'standard';

CREATE INDEX IF NOT EXISTS idx_memory_stores_kind
  ON memory_stores (tenant_id, kind);
