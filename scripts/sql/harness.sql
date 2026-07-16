-- ============================================================================
-- harness.sql
-- OMA Harness (piPy 动态工作流扩展) - 全量表结构 DDL
-- 来源: oma-platform/harness/data/workflows.db
--        + piPy-dynamic-workflows/pipy_dynamic_workflows/lib/database.py
-- 数据库: SQLite (workflows.db)
-- ============================================================================

-- ============================================================================
-- workflows — 工作流定义
-- ============================================================================

CREATE TABLE IF NOT EXISTS workflows (
    id              TEXT PRIMARY KEY,
    user_id         TEXT NOT NULL,
    name            TEXT NOT NULL,
    description     TEXT,
    yaml            TEXT NOT NULL,
    parsed_spec     TEXT NOT NULL,
    env_var_refs    TEXT DEFAULT '[]',
    is_draft        INTEGER DEFAULT 1,
    created_at      TEXT DEFAULT (datetime('now')),
    updated_at      TEXT DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_workflows_user_id ON workflows(user_id);
CREATE INDEX IF NOT EXISTS idx_workflows_created_at ON workflows(created_at DESC);

-- ============================================================================
-- workflow_executions — 工作流执行记录
-- 初始创建: 001_core
-- OMA 集成: _migrate_execution_oma_columns() 动态添加
--           oma_session_id, oma_coordinator_id 两列
-- ============================================================================

CREATE TABLE IF NOT EXISTS workflow_executions (
    id                    TEXT PRIMARY KEY,
    workflow_id           TEXT NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    user_id               TEXT NOT NULL,
    session_id            TEXT,
    status                TEXT NOT NULL DEFAULT 'pending',
    env_vars              TEXT DEFAULT '{}',
    started_at            TEXT,
    completed_at          TEXT,
    created_at            TEXT DEFAULT (datetime('now')),
    -- OMA 集成扩展列 (由 migration 动态添加)
    oma_session_id        TEXT,
    oma_coordinator_id    TEXT
);

CREATE INDEX IF NOT EXISTS idx_executions_workflow_id ON workflow_executions(workflow_id);
CREATE INDEX IF NOT EXISTS idx_executions_user_id ON workflow_executions(user_id);
CREATE INDEX IF NOT EXISTS idx_executions_session_id ON workflow_executions(session_id);
CREATE INDEX IF NOT EXISTS idx_executions_status ON workflow_executions(status);

-- ============================================================================
-- workflow_traces — 工作流步骤执行追踪
-- ============================================================================

CREATE TABLE IF NOT EXISTS workflow_traces (
    id              TEXT PRIMARY KEY,
    execution_id    TEXT NOT NULL REFERENCES workflow_executions(id) ON DELETE CASCADE,
    step_name       TEXT NOT NULL,
    step_index      INTEGER NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending',
    input           TEXT,
    output          TEXT,
    error           TEXT,
    started_at      TEXT,
    completed_at    TEXT,
    duration_ms     INTEGER,
    created_at      TEXT DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_traces_execution_id ON workflow_traces(execution_id);
CREATE INDEX IF NOT EXISTS idx_traces_step_name ON workflow_traces(step_name);
CREATE INDEX IF NOT EXISTS idx_traces_status ON workflow_traces(status);

-- ============================================================================
-- workflow_journal — 可恢复执行的 Agent 日志 (幂等重试)
-- ============================================================================

CREATE TABLE IF NOT EXISTS workflow_journal (
    id              TEXT PRIMARY KEY,
    execution_id    TEXT NOT NULL REFERENCES workflow_executions(id) ON DELETE CASCADE,
    step_hash       TEXT NOT NULL,
    step_name       TEXT NOT NULL,
    call_index      INTEGER NOT NULL DEFAULT 0,
    output          TEXT NOT NULL,
    created_at      TEXT DEFAULT (datetime('now')),
    UNIQUE(execution_id, step_hash)
);

CREATE INDEX IF NOT EXISTS idx_journal_execution_id ON workflow_journal(execution_id);
CREATE INDEX IF NOT EXISTS idx_journal_step_hash ON workflow_journal(step_hash);
