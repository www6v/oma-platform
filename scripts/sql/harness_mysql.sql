-- ============================================================================
-- harness_mysql.sql
-- OMA Harness (piPy 动态工作流扩展) - MySQL 版本 DDL
-- 来源: meta-harness/scripts/sql/harness.sql (SQLite) 的 MySQL 等价版本
-- 数据库: MySQL (managed_agent), 与 platform 表共用同一个库
-- ============================================================================
-- 注意:
--   1. `error` 是 MySQL 保留字, 必须用反引号括起来
--   2. 日期列使用 DATETIME(3), 与 Better Auth 的 user/session 等表一致;
--      pipy_dynamic_workflows 写入的是 ISO 8601 字符串, MySQL 接受
--      '2026-07-16 12:34:56.789' 这种格式
--   3. JSON 内容字段 (parsed_spec / yaml / env_vars / input / output /
--      error / env_var_refs) 使用 LONGTEXT 以防大体积
-- ============================================================================

-- ============================================================================
-- workflows — 工作流定义
-- ============================================================================

CREATE TABLE IF NOT EXISTS workflows (
    id              VARCHAR(64)   NOT NULL,
    user_id         VARCHAR(64)   NOT NULL,
    name            VARCHAR(255)  NOT NULL,
    description     TEXT,
    yaml            LONGTEXT      NOT NULL,
    parsed_spec     LONGTEXT      NOT NULL,
    env_var_refs    TEXT          DEFAULT ('[]'),
    is_draft        TINYINT(1)    DEFAULT 1,
    created_at      DATETIME(3)   DEFAULT (CURRENT_TIMESTAMP(3)),
    updated_at      DATETIME(3)   DEFAULT (CURRENT_TIMESTAMP(3)) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_workflows_user_id (user_id),
    KEY idx_workflows_created_at (created_at DESC)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================================
-- workflow_executions — 工作流执行记录
-- oma_session_id / oma_coordinator_id: OMA 集成扩展列 (初始建表即包含,
-- 不再依赖运行时 ALTER TABLE)
-- ============================================================================

CREATE TABLE IF NOT EXISTS workflow_executions (
    id                    VARCHAR(64)   NOT NULL,
    workflow_id           VARCHAR(64)   NOT NULL,
    user_id               VARCHAR(64)   NOT NULL,
    session_id            VARCHAR(64),
    status                VARCHAR(32)   NOT NULL DEFAULT 'pending',
    env_vars              TEXT          DEFAULT ('{}'),
    started_at            DATETIME(3),
    completed_at          DATETIME(3),
    created_at            DATETIME(3)   DEFAULT (CURRENT_TIMESTAMP(3)),
    oma_session_id        VARCHAR(64),
    oma_coordinator_id    VARCHAR(64),
    PRIMARY KEY (id),
    KEY idx_executions_workflow_id (workflow_id),
    KEY idx_executions_user_id (user_id),
    KEY idx_executions_session_id (session_id),
    KEY idx_executions_status (status),
    CONSTRAINT fk_executions_workflow FOREIGN KEY (workflow_id)
        REFERENCES workflows(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================================
-- workflow_traces — 工作流步骤执行追踪
-- `error` 是 MySQL 保留字, 必须用反引号
-- ============================================================================

CREATE TABLE IF NOT EXISTS workflow_traces (
    id              VARCHAR(64)   NOT NULL,
    execution_id    VARCHAR(64)   NOT NULL,
    step_name       VARCHAR(255)  NOT NULL,
    step_index      INT           NOT NULL,
    status          VARCHAR(32)   NOT NULL DEFAULT 'pending',
    input           LONGTEXT,
    output          LONGTEXT,
    `error`         LONGTEXT,
    started_at      DATETIME(3),
    completed_at    DATETIME(3),
    duration_ms     BIGINT,
    created_at      DATETIME(3)   DEFAULT (CURRENT_TIMESTAMP(3)),
    PRIMARY KEY (id),
    KEY idx_traces_execution_id (execution_id),
    KEY idx_traces_step_name (step_name),
    KEY idx_traces_status (status),
    CONSTRAINT fk_traces_execution FOREIGN KEY (execution_id)
        REFERENCES workflow_executions(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ============================================================================
-- workflow_journal — 可恢复执行的 Agent 日志 (幂等重试)
-- 同一 (execution_id, step_hash) 仅允许写入一次
-- ============================================================================

CREATE TABLE IF NOT EXISTS workflow_journal (
    id              VARCHAR(64)   NOT NULL,
    execution_id    VARCHAR(64)   NOT NULL,
    step_hash       VARCHAR(128)  NOT NULL,
    step_name       VARCHAR(255)  NOT NULL,
    call_index      INT           NOT NULL DEFAULT 0,
    output          LONGTEXT      NOT NULL,
    created_at      DATETIME(3)   DEFAULT (CURRENT_TIMESTAMP(3)),
    PRIMARY KEY (id),
    UNIQUE KEY uq_journal_execution_step (execution_id, step_hash),
    KEY idx_journal_execution_id (execution_id),
    KEY idx_journal_step_hash (step_hash),
    CONSTRAINT fk_journal_execution FOREIGN KEY (execution_id)
        REFERENCES workflow_executions(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
