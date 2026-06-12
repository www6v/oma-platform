-- GitHub per-issue and Slack per-scope session routing for webhooks.

CREATE TABLE IF NOT EXISTS github_issue_sessions (
  publication_id  TEXT NOT NULL,
  issue_key       TEXT NOT NULL,
  session_id      TEXT NOT NULL,
  status          TEXT NOT NULL DEFAULT 'active',
  created_at      INTEGER NOT NULL,
  PRIMARY KEY (publication_id, issue_key)
);

CREATE TABLE IF NOT EXISTS slack_scope_sessions (
  publication_id  TEXT NOT NULL,
  scope_key       TEXT NOT NULL,
  session_id      TEXT NOT NULL,
  status          TEXT NOT NULL DEFAULT 'active',
  created_at      INTEGER NOT NULL,
  PRIMARY KEY (publication_id, scope_key)
);
