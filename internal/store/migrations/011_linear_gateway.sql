-- Linear OAuth callback + webhook idempotency + per-issue session routing.

CREATE TABLE IF NOT EXISTS integration_webhook_deliveries (
  delivery_id     TEXT PRIMARY KEY NOT NULL,
  provider_id     TEXT NOT NULL,
  publication_id  TEXT,
  installation_id TEXT,
  received_at     INTEGER NOT NULL,
  session_id      TEXT
);

CREATE INDEX IF NOT EXISTS idx_integration_webhook_deliveries_pub
  ON integration_webhook_deliveries (publication_id);

CREATE TABLE IF NOT EXISTS linear_issue_sessions (
  publication_id  TEXT NOT NULL,
  issue_id        TEXT NOT NULL,
  session_id      TEXT NOT NULL,
  status          TEXT NOT NULL DEFAULT 'active',
  created_at      INTEGER NOT NULL,
  PRIMARY KEY (publication_id, issue_id)
);
