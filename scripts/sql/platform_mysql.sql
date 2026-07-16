-- ============================================================================
-- platform_mysql.sql
-- OMA Platform - MySQL 8.0+ DDL（从 SQLite 迁移）
-- 目标库: managed_agent
-- ============================================================================

SET NAMES utf8mb4;
SET FOREIGN_KEY_CHECKS = 0;

-- ============================================================================
-- 1. agents
-- ============================================================================
CREATE TABLE agents (
  id VARCHAR(64) PRIMARY KEY,
  tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
  config LONGTEXT NOT NULL,
  version BIGINT NOT NULL,
  created_at BIGINT NOT NULL,
  updated_at BIGINT,
  archived_at BIGINT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================================
-- 2. agent_versions
-- ============================================================================
CREATE TABLE agent_versions (
  agent_id VARCHAR(64) NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  version BIGINT NOT NULL,
  snapshot LONGTEXT NOT NULL,
  created_at BIGINT NOT NULL,
  PRIMARY KEY (agent_id, version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================================
-- 3. sessions（合并 001 + 002 + 003 + 019 + 020 + 021）
-- ============================================================================
CREATE TABLE sessions (
  id VARCHAR(64) PRIMARY KEY,
  tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
  agent_id VARCHAR(64) NOT NULL,
  agent_version BIGINT NOT NULL,
  agent_snapshot LONGTEXT NOT NULL,
  title VARCHAR(500) NOT NULL DEFAULT '',
  status VARCHAR(32) NOT NULL DEFAULT 'idle',
  turn_id VARCHAR(64),
  environment_id VARCHAR(64) NOT NULL DEFAULT '',
  environment_snapshot LONGTEXT NOT NULL,
  resources LONGTEXT NOT NULL,
  metadata LONGTEXT,
  vault_ids LONGTEXT,
  created_at BIGINT NOT NULL,
  updated_at BIGINT,
  archived_at BIGINT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================================
-- 4. session_events
-- ============================================================================
CREATE TABLE session_events (
  session_id VARCHAR(64) NOT NULL,
  seq BIGINT NOT NULL,
  event_id VARCHAR(64) NOT NULL,
  type VARCHAR(64) NOT NULL,
  payload LONGTEXT NOT NULL,
  created_at BIGINT NOT NULL,
  PRIMARY KEY (session_id, seq)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_session_events_session_seq ON session_events(session_id, seq);

-- ============================================================================
-- 5. environments
-- ============================================================================
CREATE TABLE environments (
  id VARCHAR(64) PRIMARY KEY,
  tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
  name VARCHAR(255) NOT NULL,
  description TEXT,
  status VARCHAR(32) NOT NULL DEFAULT 'ready',
  config LONGTEXT NOT NULL,
  metadata LONGTEXT,
  created_at BIGINT NOT NULL,
  updated_at BIGINT,
  archived_at BIGINT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_environments_tenant ON environments(tenant_id);

-- ============================================================================
-- 6. model_cards
-- ============================================================================
CREATE TABLE model_cards (
  id VARCHAR(64) PRIMARY KEY,
  tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
  model_id VARCHAR(255) NOT NULL,
  model VARCHAR(255) NOT NULL,
  provider VARCHAR(128) NOT NULL,
  base_url VARCHAR(1024),
  custom_headers LONGTEXT,
  api_key_cipher LONGTEXT NOT NULL,
  api_key_preview VARCHAR(255) NOT NULL,
  is_default TINYINT(1) NOT NULL DEFAULT 0,
  created_at BIGINT NOT NULL,
  updated_at BIGINT,
  archived_at BIGINT,
  UNIQUE KEY uq_model_cards_tenant_model (tenant_id, model_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_model_cards_tenant ON model_cards(tenant_id);

-- ============================================================================
-- 7. api_keys
-- ============================================================================
CREATE TABLE api_keys (
  id VARCHAR(64) PRIMARY KEY,
  tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
  user_id VARCHAR(64),
  name VARCHAR(255) NOT NULL,
  key_hash VARCHAR(255) NOT NULL,
  prefix VARCHAR(64) NOT NULL,
  source VARCHAR(64),
  created_at BIGINT NOT NULL,
  UNIQUE KEY uq_api_keys_key_hash (key_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_api_keys_tenant ON api_keys(tenant_id);

-- ============================================================================
-- 8. tenant
-- ============================================================================
CREATE TABLE tenant (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  name VARCHAR(255) NOT NULL,
  createdAt BIGINT NOT NULL,
  updatedAt BIGINT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================================
-- 9. membership
-- ============================================================================
CREATE TABLE membership (
  user_id VARCHAR(64) NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  role VARCHAR(32) NOT NULL DEFAULT 'member',
  created_at BIGINT NOT NULL,
  PRIMARY KEY (user_id, tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_membership_user ON membership(user_id);
CREATE INDEX idx_membership_tenant ON membership(tenant_id);

-- ============================================================================
-- 10. vaults
-- ============================================================================
CREATE TABLE vaults (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  name VARCHAR(255) NOT NULL,
  created_at BIGINT NOT NULL,
  updated_at BIGINT,
  archived_at BIGINT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_vaults_tenant ON vaults(tenant_id, archived_at);
CREATE INDEX idx_vaults_tenant_created_id ON vaults(tenant_id, created_at, id);

-- ============================================================================
-- 11. credentials
-- ============================================================================
CREATE TABLE credentials (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  vault_id VARCHAR(64) NOT NULL,
  display_name VARCHAR(255) NOT NULL,
  auth_type VARCHAR(64) NOT NULL,
  mcp_server_url VARCHAR(1024),
  provider VARCHAR(128),
  auth_cipher LONGTEXT NOT NULL,
  created_at BIGINT NOT NULL,
  updated_at BIGINT,
  archived_at BIGINT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_credentials_vault ON credentials(tenant_id, vault_id, archived_at);
CREATE INDEX idx_credentials_provider ON credentials(tenant_id, vault_id, provider);

-- ============================================================================
-- 12. skills
-- ============================================================================
CREATE TABLE skills (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  display_title VARCHAR(255) NOT NULL,
  name VARCHAR(255) NOT NULL,
  description TEXT NOT NULL,
  source VARCHAR(32) NOT NULL DEFAULT 'custom',
  latest_version VARCHAR(64) NOT NULL,
  created_at BIGINT NOT NULL,
  updated_at BIGINT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_skills_tenant ON skills(tenant_id, created_at);

-- ============================================================================
-- 13. skill_versions
-- ============================================================================
CREATE TABLE skill_versions (
  skill_id VARCHAR(64) NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  version VARCHAR(64) NOT NULL,
  files_json LONGTEXT NOT NULL,
  created_at BIGINT NOT NULL,
  PRIMARY KEY (skill_id, version)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_skill_versions_tenant ON skill_versions(tenant_id, skill_id);

-- ============================================================================
-- 14. pending_events
-- ============================================================================
CREATE TABLE pending_events (
  session_id VARCHAR(64) NOT NULL,
  pending_seq BIGINT NOT NULL,
  enqueued_at BIGINT NOT NULL,
  session_thread_id VARCHAR(64) NOT NULL DEFAULT 'sthr_primary',
  type VARCHAR(64) NOT NULL,
  event_id VARCHAR(64) NOT NULL,
  data LONGTEXT NOT NULL,
  cancelled_at BIGINT,
  PRIMARY KEY (session_id, pending_seq)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_pending_events_session_thread
  ON pending_events(session_id, session_thread_id, pending_seq);

-- ============================================================================
-- 15. files
-- ============================================================================
CREATE TABLE files (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  session_id VARCHAR(64),
  scope VARCHAR(64) NOT NULL,
  filename VARCHAR(512) NOT NULL,
  media_type VARCHAR(128) NOT NULL,
  size_bytes BIGINT NOT NULL,
  downloadable TINYINT(1) NOT NULL DEFAULT 0,
  blob_key VARCHAR(1024) NOT NULL,
  created_at BIGINT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_files_tenant ON files(tenant_id, created_at, id);
CREATE INDEX idx_files_session ON files(session_id, created_at, id);

-- ============================================================================
-- 16. runtimes
-- ============================================================================
CREATE TABLE runtimes (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  owner_user_id VARCHAR(64) NOT NULL,
  owner_tenant_id VARCHAR(64) NOT NULL,
  machine_id VARCHAR(255) NOT NULL,
  hostname VARCHAR(255) NOT NULL,
  os VARCHAR(64) NOT NULL,
  agents_json LONGTEXT NOT NULL,
  local_skills_json LONGTEXT NOT NULL,
  version VARCHAR(64) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'offline',
  last_heartbeat BIGINT,
  created_at BIGINT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE UNIQUE INDEX idx_runtimes_user_machine ON runtimes(owner_user_id, machine_id);
CREATE INDEX idx_runtimes_tenant ON runtimes(owner_tenant_id, created_at DESC);

-- ============================================================================
-- 17. runtime_tokens
-- ============================================================================
CREATE TABLE runtime_tokens (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  runtime_id VARCHAR(64) NOT NULL,
  token_hash VARCHAR(255) NOT NULL,
  created_by_user_id VARCHAR(64) NOT NULL,
  revoked_at BIGINT,
  last_used_at BIGINT,
  created_at BIGINT NOT NULL,
  UNIQUE KEY uq_runtime_tokens_hash (token_hash)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_runtime_tokens_runtime ON runtime_tokens(runtime_id, revoked_at);

-- ============================================================================
-- 18. connect_runtime_codes
-- ============================================================================
CREATE TABLE connect_runtime_codes (
  code VARCHAR(64) PRIMARY KEY NOT NULL,
  user_id VARCHAR(64) NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  state VARCHAR(64) NOT NULL,
  expires_at BIGINT NOT NULL,
  used_at BIGINT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_connect_runtime_codes_expires ON connect_runtime_codes(expires_at);

-- ============================================================================
-- 19. runtime_tenants
-- ============================================================================
CREATE TABLE runtime_tenants (
  runtime_id VARCHAR(64) NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  agent_api_key_id VARCHAR(64) NOT NULL,
  created_at BIGINT NOT NULL,
  revoked_at BIGINT,
  PRIMARY KEY (runtime_id, tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_runtime_tenants_runtime ON runtime_tenants(runtime_id, revoked_at);
CREATE INDEX idx_runtime_tenants_tenant ON runtime_tenants(tenant_id, revoked_at);

-- ============================================================================
-- 20. linear_installations
-- ============================================================================
CREATE TABLE linear_installations (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  user_id VARCHAR(64) NOT NULL,
  provider_id VARCHAR(32) NOT NULL DEFAULT 'linear',
  workspace_id VARCHAR(255) NOT NULL,
  workspace_name VARCHAR(255) NOT NULL,
  install_kind VARCHAR(32) NOT NULL DEFAULT 'dedicated',
  app_id VARCHAR(128),
  bot_user_id VARCHAR(255) NOT NULL,
  vault_id VARCHAR(64),
  created_at BIGINT NOT NULL,
  revoked_at BIGINT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_linear_installations_user ON linear_installations(user_id, provider_id);

-- ============================================================================
-- 21. linear_publications
-- ============================================================================
CREATE TABLE linear_publications (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  user_id VARCHAR(64) NOT NULL,
  agent_id VARCHAR(64) NOT NULL,
  installation_id VARCHAR(64) NOT NULL DEFAULT '',
  environment_id VARCHAR(64),
  mode VARCHAR(32) NOT NULL DEFAULT 'full',
  status VARCHAR(32) NOT NULL,
  persona_name VARCHAR(255) NOT NULL,
  persona_avatar_url VARCHAR(1024),
  capabilities LONGTEXT NOT NULL,
  session_granularity VARCHAR(32) NOT NULL DEFAULT 'per_issue',
  created_at BIGINT NOT NULL,
  unpublished_at BIGINT,
  client_id VARCHAR(255),
  client_secret_cipher LONGTEXT,
  webhook_secret_cipher LONGTEXT,
  signing_secret_cipher LONGTEXT,
  vault_id VARCHAR(64),
  return_url VARCHAR(1024)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_linear_publications_installation ON linear_publications(installation_id);
CREATE INDEX idx_linear_publications_user_agent ON linear_publications(user_id, agent_id);

-- ============================================================================
-- 22. linear_dispatch_rules
-- ============================================================================
CREATE TABLE linear_dispatch_rules (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  publication_id VARCHAR(64) NOT NULL,
  name VARCHAR(255) NOT NULL,
  enabled TINYINT(1) NOT NULL DEFAULT 1,
  filter_label VARCHAR(255),
  filter_states VARCHAR(1024),
  filter_project_id VARCHAR(255),
  max_concurrent BIGINT NOT NULL DEFAULT 5,
  poll_interval_seconds BIGINT NOT NULL DEFAULT 600,
  last_polled_at BIGINT,
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_linear_dispatch_rules_publication ON linear_dispatch_rules(publication_id);

-- ============================================================================
-- 23. github_installations
-- ============================================================================
CREATE TABLE github_installations (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  user_id VARCHAR(64) NOT NULL,
  provider_id VARCHAR(32) NOT NULL DEFAULT 'github',
  workspace_id VARCHAR(255) NOT NULL,
  workspace_name VARCHAR(255) NOT NULL,
  install_kind VARCHAR(32) NOT NULL DEFAULT 'dedicated',
  app_id VARCHAR(128),
  bot_user_id VARCHAR(255) NOT NULL,
  vault_id VARCHAR(64),
  created_at BIGINT NOT NULL,
  revoked_at BIGINT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_github_installations_user ON github_installations(user_id, provider_id);

-- ============================================================================
-- 24. github_publications（合并 009 + 016）
-- ============================================================================
CREATE TABLE github_publications (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  user_id VARCHAR(64) NOT NULL,
  agent_id VARCHAR(64) NOT NULL,
  installation_id VARCHAR(64) NOT NULL DEFAULT '',
  environment_id VARCHAR(64),
  mode VARCHAR(32) NOT NULL DEFAULT 'full',
  status VARCHAR(32) NOT NULL,
  persona_name VARCHAR(255) NOT NULL,
  persona_avatar_url VARCHAR(1024),
  capabilities LONGTEXT NOT NULL,
  session_granularity VARCHAR(32) NOT NULL DEFAULT 'per_issue',
  created_at BIGINT NOT NULL,
  unpublished_at BIGINT,
  client_id VARCHAR(255),
  client_secret_cipher LONGTEXT,
  webhook_secret_cipher LONGTEXT,
  signing_secret_cipher LONGTEXT,
  vault_id VARCHAR(64),
  return_url VARCHAR(1024),
  -- 016 新增
  app_oma_id VARCHAR(255),
  app_id VARCHAR(255),
  app_slug VARCHAR(255),
  bot_login VARCHAR(255),
  private_key_cipher LONGTEXT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_github_publications_installation ON github_publications(installation_id);
CREATE INDEX idx_github_publications_user_agent ON github_publications(user_id, agent_id);
CREATE INDEX idx_github_publications_app_oma_id ON github_publications(app_oma_id);

-- ============================================================================
-- 25. slack_installations
-- ============================================================================
CREATE TABLE slack_installations (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  user_id VARCHAR(64) NOT NULL,
  provider_id VARCHAR(32) NOT NULL DEFAULT 'slack',
  workspace_id VARCHAR(255) NOT NULL,
  workspace_name VARCHAR(255) NOT NULL,
  install_kind VARCHAR(32) NOT NULL DEFAULT 'dedicated',
  app_id VARCHAR(128),
  bot_user_id VARCHAR(255) NOT NULL,
  vault_id VARCHAR(64),
  created_at BIGINT NOT NULL,
  revoked_at BIGINT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_slack_installations_user ON slack_installations(user_id, provider_id);

-- ============================================================================
-- 26. slack_publications
-- ============================================================================
CREATE TABLE slack_publications (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  user_id VARCHAR(64) NOT NULL,
  agent_id VARCHAR(64) NOT NULL,
  installation_id VARCHAR(64) NOT NULL DEFAULT '',
  environment_id VARCHAR(64),
  mode VARCHAR(32) NOT NULL DEFAULT 'full',
  status VARCHAR(32) NOT NULL,
  persona_name VARCHAR(255) NOT NULL,
  persona_avatar_url VARCHAR(1024),
  capabilities LONGTEXT NOT NULL,
  session_granularity VARCHAR(32) NOT NULL DEFAULT 'per_thread',
  created_at BIGINT NOT NULL,
  unpublished_at BIGINT,
  client_id VARCHAR(255),
  client_secret_cipher LONGTEXT,
  webhook_secret_cipher LONGTEXT,
  signing_secret_cipher LONGTEXT,
  vault_id VARCHAR(64),
  return_url VARCHAR(1024)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_slack_publications_installation ON slack_publications(installation_id);
CREATE INDEX idx_slack_publications_user_agent ON slack_publications(user_id, agent_id);

-- ============================================================================
-- 27. memory_stores
-- ============================================================================
CREATE TABLE memory_stores (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  name VARCHAR(255) NOT NULL,
  description TEXT,
  created_at BIGINT NOT NULL,
  updated_at BIGINT,
  archived_at BIGINT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_memory_stores_tenant ON memory_stores(tenant_id, created_at);

-- ============================================================================
-- 28. memories（合并 010 + 013 blob_key）
-- ============================================================================
CREATE TABLE memories (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  store_id VARCHAR(64) NOT NULL,
  path VARCHAR(1024) NOT NULL,
  content LONGTEXT NOT NULL,
  content_sha256 VARCHAR(64) NOT NULL,
  etag VARCHAR(255) NOT NULL,
  size_bytes BIGINT NOT NULL,
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL,
  blob_key VARCHAR(1024),
  UNIQUE KEY uq_memories_store_path (store_id, path(255)),
  CONSTRAINT fk_memories_store FOREIGN KEY (store_id)
    REFERENCES memory_stores(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_memories_store_updated ON memories(store_id, updated_at);

-- ============================================================================
-- 29. memory_versions
-- ============================================================================
CREATE TABLE memory_versions (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  memory_id VARCHAR(64) NOT NULL,
  store_id VARCHAR(64) NOT NULL,
  operation VARCHAR(32) NOT NULL,
  path VARCHAR(1024),
  content LONGTEXT,
  content_sha256 VARCHAR(64),
  size_bytes BIGINT,
  actor_type VARCHAR(32) NOT NULL,
  actor_id VARCHAR(64) NOT NULL,
  created_at BIGINT NOT NULL,
  redacted TINYINT(1) NOT NULL DEFAULT 0,
  CONSTRAINT fk_memory_versions_store FOREIGN KEY (store_id)
    REFERENCES memory_stores(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_memory_versions_memory ON memory_versions(memory_id, created_at);
CREATE INDEX idx_memory_versions_store ON memory_versions(store_id, created_at);
CREATE INDEX idx_memory_versions_created ON memory_versions(created_at);

-- ============================================================================
-- 30. eval_runs
-- ============================================================================
CREATE TABLE eval_runs (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  agent_id VARCHAR(64) NOT NULL,
  environment_id VARCHAR(64) NOT NULL,
  suite VARCHAR(255),
  status VARCHAR(32) NOT NULL,
  started_at BIGINT NOT NULL,
  completed_at BIGINT,
  results LONGTEXT,
  score DOUBLE,
  error LONGTEXT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_eval_runs_tenant_started ON eval_runs(tenant_id, started_at);
CREATE INDEX idx_eval_runs_tenant_agent_started ON eval_runs(tenant_id, agent_id, started_at);
CREATE INDEX idx_eval_runs_tenant_environment_started ON eval_runs(tenant_id, environment_id, started_at);
CREATE INDEX idx_eval_runs_status_active ON eval_runs(status, started_at);

-- ============================================================================
-- 31. integration_webhook_deliveries
-- ============================================================================
CREATE TABLE integration_webhook_deliveries (
  delivery_id VARCHAR(64) PRIMARY KEY NOT NULL,
  provider_id VARCHAR(32) NOT NULL,
  publication_id VARCHAR(64),
  installation_id VARCHAR(64),
  received_at BIGINT NOT NULL,
  session_id VARCHAR(64)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_integration_webhook_deliveries_pub
  ON integration_webhook_deliveries(publication_id);

-- ============================================================================
-- 32. linear_issue_sessions
-- ============================================================================
CREATE TABLE linear_issue_sessions (
  publication_id VARCHAR(64) NOT NULL,
  issue_id VARCHAR(255) NOT NULL,
  session_id VARCHAR(64) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  created_at BIGINT NOT NULL,
  PRIMARY KEY (publication_id, issue_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================================
-- 33. github_issue_sessions
-- ============================================================================
CREATE TABLE github_issue_sessions (
  publication_id VARCHAR(64) NOT NULL,
  issue_key VARCHAR(255) NOT NULL,
  session_id VARCHAR(64) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  created_at BIGINT NOT NULL,
  PRIMARY KEY (publication_id, issue_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================================
-- 34. slack_scope_sessions
-- ============================================================================
CREATE TABLE slack_scope_sessions (
  publication_id VARCHAR(64) NOT NULL,
  scope_key VARCHAR(255) NOT NULL,
  session_id VARCHAR(64) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  created_at BIGINT NOT NULL,
  PRIMARY KEY (publication_id, scope_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================================
-- 35. dreams
-- ============================================================================
CREATE TABLE dreams (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  status VARCHAR(32) NOT NULL,
  input_memory_store_id VARCHAR(64) NOT NULL,
  input_session_ids LONGTEXT NOT NULL,
  output_memory_store_id VARCHAR(64),
  model VARCHAR(255) NOT NULL,
  instructions LONGTEXT,
  session_id VARCHAR(64),
  `usage` LONGTEXT NOT NULL,
  `error` LONGTEXT,
  created_at BIGINT NOT NULL,
  started_at BIGINT,
  ended_at BIGINT,
  archived_at BIGINT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_dreams_tenant_created ON dreams(tenant_id, created_at DESC);
CREATE INDEX idx_dreams_input_store ON dreams(input_memory_store_id, status);
CREATE INDEX idx_dreams_output_store ON dreams(output_memory_store_id, status);

-- ============================================================================
-- 36. session_wakeup_schedules
-- ============================================================================
CREATE TABLE session_wakeup_schedules (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  session_id VARCHAR(64) NOT NULL,
  prompt LONGTEXT NOT NULL,
  kind VARCHAR(16) NOT NULL,
  cron VARCHAR(128),
  fire_at BIGINT NOT NULL,
  parent_event_id VARCHAR(64),
  span_event_id VARCHAR(64),
  scheduled_at VARCHAR(64) NOT NULL,
  created_at BIGINT NOT NULL,
  CONSTRAINT chk_wakeup_kind CHECK (kind IN ('one_shot', 'cron'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_wakeup_fire_at ON session_wakeup_schedules(fire_at);
CREATE INDEX idx_wakeup_session ON session_wakeup_schedules(session_id);

-- ============================================================================
-- 37. teams
-- ============================================================================
CREATE TABLE teams (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  session_id VARCHAR(64) NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  name VARCHAR(255) NOT NULL,
  description TEXT,
  lead_thread_id VARCHAR(64) NOT NULL DEFAULT 'sthr_primary',
  lead_agent_id VARCHAR(64) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'active',
  created_at BIGINT NOT NULL,
  UNIQUE KEY uq_teams_session_name (session_id, name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_teams_session ON teams(session_id);

-- ============================================================================
-- 38. team_members
-- ============================================================================
CREATE TABLE team_members (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  team_id VARCHAR(64) NOT NULL,
  agent_id VARCHAR(64) NOT NULL,
  display_name VARCHAR(255) NOT NULL,
  color VARCHAR(32),
  thread_id VARCHAR(64),
  `role` LONGTEXT,
  plan_mode_required TINYINT(1) NOT NULL DEFAULT 0,
  backend_type VARCHAR(32) NOT NULL DEFAULT 'in_process',
  status VARCHAR(32) NOT NULL DEFAULT 'idle',
  joined_at BIGINT NOT NULL,
  UNIQUE KEY uq_team_members_team_name (team_id, display_name),
  CONSTRAINT fk_team_members_team FOREIGN KEY (team_id) REFERENCES teams(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_team_members_team ON team_members(team_id);

-- ============================================================================
-- 39. agent_messages
-- ============================================================================
CREATE TABLE agent_messages (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  team_id VARCHAR(64) NOT NULL,
  from_member_id VARCHAR(64) NOT NULL,
  to_member_id VARCHAR(64),
  message_type VARCHAR(32) NOT NULL DEFAULT 'text',
  body LONGTEXT NOT NULL,
  summary LONGTEXT,
  read_at BIGINT,
  created_at BIGINT NOT NULL,
  CONSTRAINT fk_agent_messages_team FOREIGN KEY (team_id) REFERENCES teams(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_agent_messages_team_recipient
  ON agent_messages(team_id, to_member_id, read_at);

-- ============================================================================
-- 40. team_tasks
-- ============================================================================
CREATE TABLE team_tasks (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  team_id VARCHAR(64) NOT NULL,
  subject VARCHAR(500) NOT NULL,
  description TEXT,
  active_form VARCHAR(255),
  owner_member_id VARCHAR(64),
  status VARCHAR(32) NOT NULL DEFAULT 'pending',
  blocks_json LONGTEXT NOT NULL,
  blocked_by_json LONGTEXT NOT NULL,
  metadata_json LONGTEXT,
  created_at BIGINT NOT NULL,
  updated_at BIGINT NOT NULL,
  CONSTRAINT fk_team_tasks_team FOREIGN KEY (team_id) REFERENCES teams(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_team_tasks_team ON team_tasks(team_id, status);

-- ============================================================================
-- 41. workspace_backups
-- ============================================================================
CREATE TABLE workspace_backups (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  tenant_id VARCHAR(64) NOT NULL,
  environment_id VARCHAR(64) NOT NULL,
  backup_handle VARCHAR(512) NOT NULL,
  created_at BIGINT NOT NULL,
  expires_at BIGINT NOT NULL,
  source_session_id VARCHAR(64)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_workspace_backups_scope_recent
  ON workspace_backups(tenant_id, environment_id, created_at DESC);
CREATE INDEX idx_workspace_backups_expires ON workspace_backups(expires_at);
CREATE INDEX idx_workspace_backups_session
  ON workspace_backups(source_session_id, created_at DESC);

-- ============================================================================
-- 42. system_runtimes
-- ============================================================================
CREATE TABLE system_runtimes (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  agent VARCHAR(255) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'warming',
  sandbox_id VARCHAR(255),
  created_at BIGINT NOT NULL,
  last_heartbeat_at BIGINT,
  idle_since_at BIGINT,
  stopped_at BIGINT
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_system_runtimes_tenant_agent
  ON system_runtimes(tenant_id, agent, created_at DESC);
CREATE INDEX idx_system_runtimes_status
  ON system_runtimes(status, idle_since_at);

-- ============================================================================
-- 43. system_runtime_leases
-- ============================================================================
CREATE TABLE system_runtime_leases (
  id VARCHAR(64) PRIMARY KEY NOT NULL,
  system_runtime_id VARCHAR(64) NOT NULL,
  tenant_id VARCHAR(64) NOT NULL,
  session_id VARCHAR(64) NOT NULL,
  acquired_at BIGINT NOT NULL,
  released_at BIGINT,
  idle_ttl_at BIGINT,
  CONSTRAINT fk_lease_runtime FOREIGN KEY (system_runtime_id)
    REFERENCES system_runtimes(id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE INDEX idx_system_runtime_leases_runtime
  ON system_runtime_leases(system_runtime_id, released_at);
CREATE INDEX idx_system_runtime_leases_session
  ON system_runtime_leases(session_id, released_at);
CREATE INDEX idx_system_runtime_leases_ttl
  ON system_runtime_leases(idle_ttl_at);

-- ============================================================================
-- 44. schema_migrations
-- ============================================================================
CREATE TABLE schema_migrations (
  name VARCHAR(255) PRIMARY KEY,
  applied_at BIGINT NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================================
-- 45. Better Auth tables (owned by oma-auth / auth-sidecar, but live in the
--     same MySQL database as the platform). Auto-created by better-auth on
--     first start if missing, but listed here so a fresh `platform_mysql.sql`
--     import is sufficient to bring up the whole stack.
--
--     IMPORTANT: better-auth's MySQL adapter writes datetime strings (e.g.
--     "2026-07-23 18:17:53.596"), NOT unix timestamps. Date columns MUST be
--     DATETIME(3); BIGINT would cause "Data truncated" errors at runtime.
-- ============================================================================
CREATE TABLE IF NOT EXISTS `user` (
  `id` VARCHAR(64) NOT NULL,
  `email` VARCHAR(255) NOT NULL,
  `emailVerified` BOOLEAN NOT NULL DEFAULT FALSE,
  `name` VARCHAR(255) NOT NULL,
  `image` TEXT,
  `tenantId` VARCHAR(64),
  `role` VARCHAR(32),
  `createdAt` DATETIME(3) NOT NULL,
  `updatedAt` DATETIME(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `user_email_unique` (`email`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `session` (
  `id` VARCHAR(64) NOT NULL,
  `userId` VARCHAR(64) NOT NULL,
  `token` VARCHAR(255) NOT NULL,
  `expiresAt` DATETIME(3) NOT NULL,
  `ipAddress` VARCHAR(255),
  `userAgent` TEXT,
  `createdAt` DATETIME(3) NOT NULL,
  `updatedAt` DATETIME(3) NOT NULL,
  PRIMARY KEY (`id`),
  UNIQUE KEY `session_token_unique` (`token`),
  KEY `session_user_idx` (`userId`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `account` (
  `id` VARCHAR(64) NOT NULL,
  `userId` VARCHAR(64) NOT NULL,
  `accountId` VARCHAR(255) NOT NULL,
  `providerId` VARCHAR(255) NOT NULL,
  `accessToken` TEXT,
  `refreshToken` TEXT,
  `idToken` TEXT,
  `accessTokenExpiresAt` DATETIME(3),
  `refreshTokenExpiresAt` DATETIME(3),
  `scope` VARCHAR(255),
  `password` TEXT,
  `createdAt` DATETIME(3) NOT NULL,
  `updatedAt` DATETIME(3) NOT NULL,
  PRIMARY KEY (`id`),
  KEY `account_user_idx` (`userId`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE IF NOT EXISTS `verification` (
  `id` VARCHAR(64) NOT NULL,
  `identifier` VARCHAR(255) NOT NULL,
  `value` TEXT NOT NULL,
  `expiresAt` DATETIME(3) NOT NULL,
  `createdAt` DATETIME(3),
  `updatedAt` DATETIME(3),
  PRIMARY KEY (`id`),
  KEY `verification_identifier_idx` (`identifier`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

SET FOREIGN_KEY_CHECKS = 1;
