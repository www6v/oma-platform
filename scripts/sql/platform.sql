-- ============================================================================
-- platform.sql
-- OMA Platform - 全量表结构 DDL
-- 来源: meta-harness/internal/store/migrations/001 ~ 023
-- 数据库: SQLite (oma.db)
-- ============================================================================

-- ============================================================================
-- 001_core.sql — 核心表: agents / agent_versions / sessions / session_events
-- ============================================================================

CREATE TABLE agents (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL DEFAULT 'default',
  config TEXT NOT NULL,
  version INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  updated_at INTEGER,
  archived_at INTEGER
);

CREATE TABLE agent_versions (
  agent_id TEXT NOT NULL,
  tenant_id TEXT NOT NULL,
  version INTEGER NOT NULL,
  snapshot TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (agent_id, version)
);

-- sessions 表: 初始版本 + 002(environment) + 003(archived_at) + 019(resources)
--              + 020(metadata) + 021(vault_ids) 合并后最终结构
CREATE TABLE sessions (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL DEFAULT 'default',
  agent_id TEXT NOT NULL,
  agent_version INTEGER NOT NULL,
  agent_snapshot TEXT NOT NULL,
  title TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'idle',
  turn_id TEXT,
  environment_id TEXT NOT NULL DEFAULT '',
  environment_snapshot TEXT NOT NULL DEFAULT '{}',
  resources TEXT NOT NULL DEFAULT '[]',
  metadata TEXT,
  vault_ids TEXT,
  created_at INTEGER NOT NULL,
  updated_at INTEGER,
  archived_at INTEGER
);

CREATE TABLE session_events (
  session_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  event_id TEXT NOT NULL,
  type TEXT NOT NULL,
  payload TEXT NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (session_id, seq)
);

CREATE INDEX idx_session_events_session_seq ON session_events(session_id, seq);

-- ============================================================================
-- 002_p1_environments_model_cards.sql — 环境 & 模型卡
-- ============================================================================

CREATE TABLE environments (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL DEFAULT 'default',
  name TEXT NOT NULL,
  description TEXT,
  status TEXT NOT NULL DEFAULT 'ready',
  config TEXT NOT NULL,
  metadata TEXT,
  created_at INTEGER NOT NULL,
  updated_at INTEGER,
  archived_at INTEGER
);

CREATE INDEX idx_environments_tenant ON environments(tenant_id);

CREATE TABLE model_cards (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL DEFAULT 'default',
  model_id TEXT NOT NULL,
  model TEXT NOT NULL,
  provider TEXT NOT NULL,
  base_url TEXT,
  custom_headers TEXT,
  api_key_cipher TEXT NOT NULL,
  api_key_preview TEXT NOT NULL,
  is_default INTEGER NOT NULL DEFAULT 0,
  created_at INTEGER NOT NULL,
  updated_at INTEGER,
  archived_at INTEGER,
  UNIQUE (tenant_id, model_id)
);

CREATE UNIQUE INDEX idx_model_cards_default
  ON model_cards(tenant_id)
  WHERE is_default = 1 AND archived_at IS NULL;

CREATE INDEX idx_model_cards_tenant ON model_cards(tenant_id);

-- ============================================================================
-- 003_p1_console.sql — API Keys
-- ============================================================================

CREATE TABLE api_keys (
  id TEXT PRIMARY KEY,
  tenant_id TEXT NOT NULL DEFAULT 'default',
  user_id TEXT,
  name TEXT NOT NULL,
  key_hash TEXT NOT NULL UNIQUE,
  prefix TEXT NOT NULL,
  source TEXT,
  created_at INTEGER NOT NULL
);

CREATE INDEX idx_api_keys_tenant ON api_keys(tenant_id);
CREATE INDEX idx_api_keys_hash ON api_keys(key_hash);

-- ============================================================================
-- 004_auth_tenants.sql — 租户 & 成员
-- ============================================================================

CREATE TABLE IF NOT EXISTS tenant (
  id TEXT PRIMARY KEY NOT NULL,
  name TEXT NOT NULL,
  "createdAt" INTEGER NOT NULL,
  "updatedAt" INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS membership (
  user_id TEXT NOT NULL,
  tenant_id TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'member',
  created_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, tenant_id)
);

CREATE INDEX IF NOT EXISTS idx_membership_user ON membership (user_id);
CREATE INDEX IF NOT EXISTS idx_membership_tenant ON membership (tenant_id);

-- ============================================================================
-- 005_vaults_credentials_skills.sql — 保险库 / 凭据 / 技能
-- ============================================================================

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

-- ============================================================================
-- 006_pending_events.sql — 待处理事件
-- ============================================================================

CREATE TABLE pending_events (
  session_id TEXT NOT NULL,
  pending_seq INTEGER NOT NULL,
  enqueued_at INTEGER NOT NULL,
  session_thread_id TEXT NOT NULL DEFAULT 'sthr_primary',
  type TEXT NOT NULL,
  event_id TEXT NOT NULL,
  data TEXT NOT NULL,
  cancelled_at INTEGER,
  PRIMARY KEY (session_id, pending_seq)
);

CREATE INDEX idx_pending_events_session_thread
  ON pending_events(session_id, session_thread_id, pending_seq);

-- ============================================================================
-- 007_files.sql — 文件管理
-- ============================================================================

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

-- ============================================================================
-- 008_runtimes.sql — 运行时注册 / Token / 连接码 / 租户绑定
-- ============================================================================

CREATE TABLE IF NOT EXISTS runtimes (
  id                TEXT PRIMARY KEY NOT NULL,
  owner_user_id     TEXT NOT NULL,
  owner_tenant_id   TEXT NOT NULL,
  machine_id        TEXT NOT NULL,
  hostname          TEXT NOT NULL,
  os                TEXT NOT NULL,
  agents_json       TEXT NOT NULL DEFAULT '[]',
  local_skills_json TEXT NOT NULL DEFAULT '{}',
  version           TEXT NOT NULL,
  status            TEXT NOT NULL DEFAULT 'offline',
  last_heartbeat    INTEGER,
  created_at        INTEGER NOT NULL
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_runtimes_user_machine
  ON runtimes (owner_user_id, machine_id);
CREATE INDEX IF NOT EXISTS idx_runtimes_tenant
  ON runtimes (owner_tenant_id, created_at DESC);

CREATE TABLE IF NOT EXISTS runtime_tokens (
  id                  TEXT PRIMARY KEY NOT NULL,
  runtime_id          TEXT NOT NULL,
  token_hash          TEXT NOT NULL UNIQUE,
  created_by_user_id  TEXT NOT NULL,
  revoked_at          INTEGER,
  last_used_at        INTEGER,
  created_at          INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_runtime_tokens_runtime
  ON runtime_tokens (runtime_id, revoked_at);

CREATE TABLE IF NOT EXISTS connect_runtime_codes (
  code        TEXT PRIMARY KEY NOT NULL,
  user_id     TEXT NOT NULL,
  tenant_id   TEXT NOT NULL,
  state       TEXT NOT NULL,
  expires_at  INTEGER NOT NULL,
  used_at     INTEGER
);

CREATE INDEX IF NOT EXISTS idx_connect_runtime_codes_expires
  ON connect_runtime_codes (expires_at);

CREATE TABLE IF NOT EXISTS runtime_tenants (
  runtime_id        TEXT NOT NULL,
  tenant_id         TEXT NOT NULL,
  agent_api_key_id  TEXT NOT NULL,
  created_at        INTEGER NOT NULL,
  revoked_at        INTEGER,
  PRIMARY KEY (runtime_id, tenant_id)
);

CREATE INDEX IF NOT EXISTS idx_runtime_tenants_runtime
  ON runtime_tenants (runtime_id, revoked_at);
CREATE INDEX IF NOT EXISTS idx_runtime_tenants_tenant
  ON runtime_tenants (tenant_id, revoked_at);

-- ============================================================================
-- 009_integrations.sql — 集成: Linear / GitHub / Slack 安装 & 发布
-- ============================================================================

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

-- linear_publications 最终结构 (009 创建 + 016 新增 GitHub proxy 字段)
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

-- github_publications 最终结构 (009 创建 + 016 新增 proxy 字段)
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
  return_url            TEXT,
  -- 016_github_install_proxy.sql 新增字段
  app_oma_id            TEXT,
  app_id                TEXT,
  app_slug              TEXT,
  bot_login             TEXT,
  private_key_cipher    TEXT
);

CREATE INDEX IF NOT EXISTS idx_github_publications_installation
  ON github_publications (installation_id);
CREATE INDEX IF NOT EXISTS idx_github_publications_user_agent
  ON github_publications (user_id, agent_id);
CREATE INDEX IF NOT EXISTS idx_github_publications_app_oma_id
  ON github_publications (app_oma_id)
  WHERE app_oma_id IS NOT NULL;

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

-- ============================================================================
-- 010_memory_evals.sql — 记忆存储 & 评估
-- 013_memory_blobs.sql — memories 新增 blob_key 列
-- ============================================================================

CREATE TABLE IF NOT EXISTS memory_stores (
  id            TEXT PRIMARY KEY NOT NULL,
  tenant_id     TEXT NOT NULL,
  name          TEXT NOT NULL,
  description   TEXT,
  created_at    INTEGER NOT NULL,
  updated_at    INTEGER,
  archived_at   INTEGER
);

CREATE INDEX IF NOT EXISTS idx_memory_stores_tenant
  ON memory_stores (tenant_id, created_at);

-- memories 最终结构 (010 创建 + 013 新增 blob_key)
CREATE TABLE IF NOT EXISTS memories (
  id              TEXT PRIMARY KEY NOT NULL,
  store_id        TEXT NOT NULL,
  path            TEXT NOT NULL,
  content         TEXT NOT NULL DEFAULT '',
  content_sha256  TEXT NOT NULL,
  etag            TEXT NOT NULL,
  size_bytes      INTEGER NOT NULL,
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL,
  blob_key        TEXT,
  FOREIGN KEY (store_id) REFERENCES memory_stores(id) ON DELETE CASCADE,
  UNIQUE (store_id, path)
);

CREATE INDEX IF NOT EXISTS idx_memories_store_updated
  ON memories (store_id, updated_at);

CREATE TABLE IF NOT EXISTS memory_versions (
  id              TEXT PRIMARY KEY NOT NULL,
  memory_id       TEXT NOT NULL,
  store_id        TEXT NOT NULL,
  operation       TEXT NOT NULL,
  path            TEXT,
  content         TEXT,
  content_sha256  TEXT,
  size_bytes      INTEGER,
  actor_type      TEXT NOT NULL,
  actor_id        TEXT NOT NULL,
  created_at      INTEGER NOT NULL,
  redacted        INTEGER NOT NULL DEFAULT 0,
  FOREIGN KEY (store_id) REFERENCES memory_stores(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_memory_versions_memory
  ON memory_versions (memory_id, created_at);
CREATE INDEX IF NOT EXISTS idx_memory_versions_store
  ON memory_versions (store_id, created_at);
-- 013 新增索引
CREATE INDEX IF NOT EXISTS idx_memory_versions_created
  ON memory_versions (created_at);

CREATE TABLE IF NOT EXISTS eval_runs (
  id               TEXT PRIMARY KEY NOT NULL,
  tenant_id        TEXT NOT NULL,
  agent_id         TEXT NOT NULL,
  environment_id   TEXT NOT NULL,
  suite            TEXT,
  status           TEXT NOT NULL,
  started_at       INTEGER NOT NULL,
  completed_at     INTEGER,
  results          TEXT,
  score            REAL,
  error            TEXT
);

CREATE INDEX IF NOT EXISTS idx_eval_runs_tenant_started
  ON eval_runs (tenant_id, started_at);
CREATE INDEX IF NOT EXISTS idx_eval_runs_tenant_agent_started
  ON eval_runs (tenant_id, agent_id, started_at);
CREATE INDEX IF NOT EXISTS idx_eval_runs_tenant_environment_started
  ON eval_runs (tenant_id, environment_id, started_at);
CREATE INDEX IF NOT EXISTS idx_eval_runs_status_active
  ON eval_runs (status, started_at);

-- ============================================================================
-- 011_linear_gateway.sql — Webhook 幂等 & Linear Issue Session 路由
-- ============================================================================

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

-- ============================================================================
-- 012_integration_webhooks.sql — GitHub / Slack Session 路由
-- ============================================================================

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

-- ============================================================================
-- 014_dreams.sql — 梦境 (自动化记忆整理)
-- ============================================================================

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

-- ============================================================================
-- 015_session_wakeups.sql — 会话唤醒调度
-- ============================================================================

CREATE TABLE IF NOT EXISTS session_wakeup_schedules (
  id              TEXT PRIMARY KEY NOT NULL,
  tenant_id       TEXT NOT NULL,
  session_id      TEXT NOT NULL,
  prompt          TEXT NOT NULL,
  kind            TEXT NOT NULL CHECK (kind IN ('one_shot', 'cron')),
  cron            TEXT,
  fire_at         INTEGER NOT NULL,
  parent_event_id TEXT,
  span_event_id   TEXT,
  scheduled_at    TEXT NOT NULL,
  created_at      INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_wakeup_fire_at
  ON session_wakeup_schedules (fire_at);
CREATE INDEX IF NOT EXISTS idx_wakeup_session
  ON session_wakeup_schedules (session_id);

-- ============================================================================
-- 017_teams.sql — 团队 / 成员 / 消息
-- ============================================================================

CREATE TABLE IF NOT EXISTS teams (
  id              TEXT PRIMARY KEY NOT NULL,
  session_id      TEXT NOT NULL,
  tenant_id       TEXT NOT NULL,
  name            TEXT NOT NULL,
  description     TEXT,
  lead_thread_id  TEXT NOT NULL DEFAULT 'sthr_primary',
  lead_agent_id   TEXT NOT NULL,
  status          TEXT NOT NULL DEFAULT 'active',
  created_at      INTEGER NOT NULL,
  UNIQUE(session_id, name)
);

CREATE INDEX IF NOT EXISTS idx_teams_session
  ON teams (session_id);

CREATE TABLE IF NOT EXISTS team_members (
  id              TEXT PRIMARY KEY NOT NULL,
  team_id         TEXT NOT NULL REFERENCES teams(id),
  agent_id        TEXT NOT NULL,
  display_name    TEXT NOT NULL,
  color           TEXT,
  thread_id       TEXT,
  role            TEXT,
  plan_mode_required INTEGER NOT NULL DEFAULT 0,
  backend_type    TEXT NOT NULL DEFAULT 'in_process',
  status          TEXT NOT NULL DEFAULT 'idle',
  joined_at       INTEGER NOT NULL,
  UNIQUE(team_id, display_name)
);

CREATE INDEX IF NOT EXISTS idx_team_members_team
  ON team_members (team_id);

CREATE TABLE IF NOT EXISTS agent_messages (
  id              TEXT PRIMARY KEY NOT NULL,
  team_id         TEXT NOT NULL REFERENCES teams(id),
  from_member_id  TEXT NOT NULL,
  to_member_id    TEXT,
  message_type    TEXT NOT NULL DEFAULT 'text',
  body            TEXT NOT NULL,
  summary         TEXT,
  read_at         INTEGER,
  created_at      INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_agent_messages_team_recipient
  ON agent_messages (team_id, to_member_id, read_at);

-- ============================================================================
-- 018_team_tasks.sql — 团队任务
-- ============================================================================

CREATE TABLE IF NOT EXISTS team_tasks (
  id              TEXT PRIMARY KEY NOT NULL,
  team_id         TEXT NOT NULL REFERENCES teams(id),
  subject         TEXT NOT NULL,
  description     TEXT,
  active_form     TEXT,
  owner_member_id TEXT,
  status          TEXT NOT NULL DEFAULT 'pending',
  blocks_json     TEXT NOT NULL DEFAULT '[]',
  blocked_by_json TEXT NOT NULL DEFAULT '[]',
  metadata_json   TEXT,
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_team_tasks_team
  ON team_tasks (team_id, status);

-- ============================================================================
-- 022_workspace_backups.sql — 工作区备份
-- ============================================================================

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

-- ============================================================================
-- 023_system_runtimes.sql — 系统运行时池
-- ============================================================================

CREATE TABLE IF NOT EXISTS system_runtimes (
  id                TEXT PRIMARY KEY NOT NULL,
  tenant_id         TEXT    NOT NULL,
  agent             TEXT    NOT NULL,
  status            TEXT    NOT NULL DEFAULT 'warming',
  sandbox_id        TEXT,
  created_at        INTEGER NOT NULL,
  last_heartbeat_at INTEGER,
  idle_since_at     INTEGER,
  stopped_at        INTEGER
);

CREATE INDEX IF NOT EXISTS idx_system_runtimes_tenant_agent
  ON system_runtimes (tenant_id, agent, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_system_runtimes_status
  ON system_runtimes (status, idle_since_at);

CREATE TABLE IF NOT EXISTS system_runtime_leases (
  id                TEXT PRIMARY KEY NOT NULL,
  system_runtime_id TEXT NOT NULL,
  tenant_id         TEXT NOT NULL,
  session_id        TEXT NOT NULL,
  acquired_at       INTEGER NOT NULL,
  released_at       INTEGER,
  idle_ttl_at       INTEGER,
  FOREIGN KEY (system_runtime_id) REFERENCES system_runtimes (id)
);

CREATE INDEX IF NOT EXISTS idx_system_runtime_leases_runtime
  ON system_runtime_leases (system_runtime_id, released_at);
CREATE INDEX IF NOT EXISTS idx_system_runtime_leases_session
  ON system_runtime_leases (session_id, released_at);
CREATE INDEX IF NOT EXISTS idx_system_runtime_leases_ttl
  ON system_runtime_leases (idle_ttl_at) WHERE released_at IS NOT NULL;

-- ============================================================================
-- Better Auth 相关表（由 oma-auth / auth-sidecar 使用，但与 platform 共用同一
-- 个数据库）。better-auth 启动时会自动创建，这里列出便于整体导入 / 对账。
-- ============================================================================

CREATE TABLE IF NOT EXISTS "user" (
  "id"            TEXT PRIMARY KEY NOT NULL,
  "email"         TEXT NOT NULL UNIQUE,
  "emailVerified" INTEGER NOT NULL DEFAULT 0,
  "name"          TEXT NOT NULL,
  "image"         TEXT,
  "tenantId"      TEXT,
  "role"          TEXT,
  "createdAt"     INTEGER NOT NULL,
  "updatedAt"     INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS "session" (
  "id"          TEXT PRIMARY KEY NOT NULL,
  "userId"      TEXT NOT NULL REFERENCES "user"("id") ON DELETE CASCADE,
  "token"       TEXT NOT NULL UNIQUE,
  "expiresAt"   INTEGER NOT NULL,
  "ipAddress"   TEXT,
  "userAgent"   TEXT,
  "createdAt"   INTEGER NOT NULL,
  "updatedAt"   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS "account" (
  "id"                     TEXT PRIMARY KEY NOT NULL,
  "userId"                 TEXT NOT NULL REFERENCES "user"("id") ON DELETE CASCADE,
  "accountId"              TEXT NOT NULL,
  "providerId"             TEXT NOT NULL,
  "accessToken"            TEXT,
  "refreshToken"           TEXT,
  "idToken"                TEXT,
  "accessTokenExpiresAt"   INTEGER,
  "refreshTokenExpiresAt"  INTEGER,
  "scope"                  TEXT,
  "password"               TEXT,
  "createdAt"              INTEGER NOT NULL,
  "updatedAt"              INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS "verification" (
  "id"          TEXT PRIMARY KEY NOT NULL,
  "identifier"  TEXT NOT NULL,
  "value"       TEXT NOT NULL,
  "expiresAt"   INTEGER NOT NULL,
  "createdAt"   INTEGER,
  "updatedAt"   INTEGER
);

-- ============================================================================
-- 内部迁移追踪表
-- ============================================================================

CREATE TABLE IF NOT EXISTS schema_migrations (
  name TEXT PRIMARY KEY,
  applied_at INTEGER NOT NULL
);
