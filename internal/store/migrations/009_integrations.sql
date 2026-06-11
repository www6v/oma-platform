-- Integrations persistence (Linear / GitHub / Slack) — Console wire-compatible.

CREATE TABLE IF NOT EXISTS linear_installations (
  id              TEXT PRIMARY KEY NOT NULL,
  tenant_id       TEXT NOT NULL,
  user_id         TEXT NOT NULL,
  provider_id     TEXT NOT NULL DEFAULT 'linear',
  workspace_id    TEXT NOT NULL,
  workspace_name  TEXT NOT NULL,
  install_kind    TEXT NOT NULL DEFAULT 'dedicated',
  app_id          TEXT,
  bot_user_id     TEXT NOT NULL,
  vault_id        TEXT,
  created_at      INTEGER NOT NULL,
  revoked_at      INTEGER
);

CREATE INDEX IF NOT EXISTS idx_linear_installations_user
  ON linear_installations (user_id, provider_id);

CREATE TABLE IF NOT EXISTS linear_publications (
  id                    TEXT PRIMARY KEY NOT NULL,
  tenant_id             TEXT NOT NULL,
  user_id               TEXT NOT NULL,
  agent_id              TEXT NOT NULL,
  installation_id       TEXT NOT NULL DEFAULT '',
  environment_id        TEXT,
  mode                  TEXT NOT NULL DEFAULT 'full',
  status                TEXT NOT NULL,
  persona_name          TEXT NOT NULL,
  persona_avatar_url    TEXT,
  capabilities          TEXT NOT NULL DEFAULT '[]',
  session_granularity   TEXT NOT NULL DEFAULT 'per_issue',
  created_at            INTEGER NOT NULL,
  unpublished_at        INTEGER,
  client_id             TEXT,
  client_secret_cipher  TEXT,
  webhook_secret_cipher TEXT,
  signing_secret_cipher TEXT,
  vault_id              TEXT,
  return_url            TEXT
);

CREATE INDEX IF NOT EXISTS idx_linear_publications_installation
  ON linear_publications (installation_id);

CREATE INDEX IF NOT EXISTS idx_linear_publications_user_agent
  ON linear_publications (user_id, agent_id);

CREATE INDEX IF NOT EXISTS idx_linear_publications_pending
  ON linear_publications (user_id, status)
  WHERE unpublished_at IS NULL;

CREATE TABLE IF NOT EXISTS linear_dispatch_rules (
  id                    TEXT PRIMARY KEY NOT NULL,
  tenant_id             TEXT NOT NULL,
  publication_id        TEXT NOT NULL,
  name                  TEXT NOT NULL,
  enabled               INTEGER NOT NULL DEFAULT 1,
  filter_label          TEXT,
  filter_states         TEXT,
  filter_project_id     TEXT,
  max_concurrent        INTEGER NOT NULL DEFAULT 5,
  poll_interval_seconds INTEGER NOT NULL DEFAULT 600,
  last_polled_at        INTEGER,
  created_at            INTEGER NOT NULL,
  updated_at            INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_linear_dispatch_rules_publication
  ON linear_dispatch_rules (publication_id);

CREATE TABLE IF NOT EXISTS github_installations (
  id              TEXT PRIMARY KEY NOT NULL,
  tenant_id       TEXT NOT NULL,
  user_id         TEXT NOT NULL,
  provider_id     TEXT NOT NULL DEFAULT 'github',
  workspace_id    TEXT NOT NULL,
  workspace_name  TEXT NOT NULL,
  install_kind    TEXT NOT NULL DEFAULT 'dedicated',
  app_id          TEXT,
  bot_user_id     TEXT NOT NULL,
  vault_id        TEXT,
  created_at      INTEGER NOT NULL,
  revoked_at      INTEGER
);

CREATE INDEX IF NOT EXISTS idx_github_installations_user
  ON github_installations (user_id, provider_id);

CREATE TABLE IF NOT EXISTS github_publications (
  id                    TEXT PRIMARY KEY NOT NULL,
  tenant_id             TEXT NOT NULL,
  user_id               TEXT NOT NULL,
  agent_id              TEXT NOT NULL,
  installation_id       TEXT NOT NULL DEFAULT '',
  environment_id        TEXT,
  mode                  TEXT NOT NULL DEFAULT 'full',
  status                TEXT NOT NULL,
  persona_name          TEXT NOT NULL,
  persona_avatar_url    TEXT,
  capabilities          TEXT NOT NULL DEFAULT '[]',
  session_granularity   TEXT NOT NULL DEFAULT 'per_issue',
  created_at            INTEGER NOT NULL,
  unpublished_at        INTEGER,
  client_id             TEXT,
  client_secret_cipher  TEXT,
  webhook_secret_cipher TEXT,
  signing_secret_cipher TEXT,
  vault_id              TEXT,
  return_url            TEXT
);

CREATE INDEX IF NOT EXISTS idx_github_publications_installation
  ON github_publications (installation_id);

CREATE INDEX IF NOT EXISTS idx_github_publications_user_agent
  ON github_publications (user_id, agent_id);

CREATE TABLE IF NOT EXISTS slack_installations (
  id              TEXT PRIMARY KEY NOT NULL,
  tenant_id       TEXT NOT NULL,
  user_id         TEXT NOT NULL,
  provider_id     TEXT NOT NULL DEFAULT 'slack',
  workspace_id    TEXT NOT NULL,
  workspace_name  TEXT NOT NULL,
  install_kind    TEXT NOT NULL DEFAULT 'dedicated',
  app_id          TEXT,
  bot_user_id     TEXT NOT NULL,
  vault_id        TEXT,
  created_at      INTEGER NOT NULL,
  revoked_at      INTEGER
);

CREATE INDEX IF NOT EXISTS idx_slack_installations_user
  ON slack_installations (user_id, provider_id);

CREATE TABLE IF NOT EXISTS slack_publications (
  id                    TEXT PRIMARY KEY NOT NULL,
  tenant_id             TEXT NOT NULL,
  user_id               TEXT NOT NULL,
  agent_id              TEXT NOT NULL,
  installation_id       TEXT NOT NULL DEFAULT '',
  environment_id        TEXT,
  mode                  TEXT NOT NULL DEFAULT 'full',
  status                TEXT NOT NULL,
  persona_name          TEXT NOT NULL,
  persona_avatar_url    TEXT,
  capabilities          TEXT NOT NULL DEFAULT '[]',
  session_granularity   TEXT NOT NULL DEFAULT 'per_thread',
  created_at            INTEGER NOT NULL,
  unpublished_at        INTEGER,
  client_id             TEXT,
  client_secret_cipher  TEXT,
  webhook_secret_cipher TEXT,
  signing_secret_cipher TEXT,
  vault_id              TEXT,
  return_url            TEXT
);

CREATE INDEX IF NOT EXISTS idx_slack_publications_installation
  ON slack_publications (installation_id);

CREATE INDEX IF NOT EXISTS idx_slack_publications_user_agent
  ON slack_publications (user_id, agent_id);
