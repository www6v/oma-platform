#!/usr/bin/env python3
"""
One-shot migration: workflows SQLite → MySQL.

Reads:
    By default ./data/workflows.db (piPy-dynamic-workflows project).
    Override with WORKFLOWS_SQLITE env var. Only migrates this one file;
    sibling workflows.db files (console/, harness/, oma-platform/data/)
    are independent copies and can each be migrated by passing their path
    via WORKFLOWS_SQLITE.

Writes into the MySQL database pointed to by DATABASE_URL (same one that
oma-platform uses). The four workflow tables (workflows, workflow_executions,
workflow_traces, workflow_journal) are created if missing; existing rows are
skipped via INSERT IGNORE so the script is safe to re-run.

Usage:
    pip install pymysql  # one-off
    python3 migrate-workflows-sqlite-to-mysql.py
    # or override paths / url:
    WORKFLOWS_SQLITE=/path/to/workflows.db \
    DATABASE_URL=mysql+aiomysql://user:pass@host/db \
    python3 migrate-workflows-sqlite-to-mysql.py
"""
from __future__ import annotations

import os
import re
import sqlite3
import sys
from contextlib import closing
from urllib.parse import urlparse

try:
    import pymysql
    from pymysql.cursors import DictCursor
except ImportError:
    sys.exit("pymysql is required: pip install pymysql")


# ---------------------------------------------------------------------------
# DATABASE_URL parsing
# ---------------------------------------------------------------------------

def parse_database_url(raw: str) -> dict:
    if "@tcp(" in raw:
        m = re.match(
            r"^([^:]+):([^@]*)@tcp\(([^):]+):(\d+)\)/([^?]+)(\?.*)?$",
            raw,
        )
        if not m:
            raise ValueError(f"cannot parse mysql DSN: {raw}")
        return {
            "host": m.group(3),
            "port": int(m.group(4)),
            "user": _urldecode(m.group(1)),
            "password": _urldecode(m.group(2)),
            "database": m.group(5),
        }
    s = raw
    for prefix in ("mysql+aiomysql://", "mysql+mysqlconnector://", "mysql://"):
        if s.startswith(prefix):
            s = "mysql://" + s[len(prefix):]
            break
    u = urlparse(s)
    return {
        "host": u.hostname,
        "port": u.port or 3306,
        "user": _urldecode(u.username),
        "password": _urldecode(u.password),
        "database": u.path.lstrip("/"),
    }


def _urldecode(s: str | None) -> str:
    if not s:
        return ""
    from urllib.parse import unquote
    return unquote(s)


# ---------------------------------------------------------------------------
# MySQL DDL for workflow tables (mirror of harness_mysql.sql / database.py)
# ---------------------------------------------------------------------------

WORKFLOW_DDL = [
    # Note: `error` is a MySQL reserved word and must be quoted.
    """
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
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
    """,
    """
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
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
    """,
    """
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
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
    """,
    """
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
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
    """,
]


# ---------------------------------------------------------------------------
# Migration
# ---------------------------------------------------------------------------

def migrate(workflows_sqlite: str, database_url: str) -> None:
    cfg = parse_database_url(database_url)
    print(f"→ mysql {cfg['user']}@{cfg['host']}:{cfg['port']}/{cfg['database']}")

    mysql = pymysql.connect(
        host=cfg["host"],
        port=cfg["port"],
        user=cfg["user"],
        password=cfg["password"],
        database=cfg["database"],
        charset="utf8mb4",
        autocommit=False,
        cursorclass=DictCursor,
    )

    # 1. Ensure workflow tables exist.
    with closing(mysql.cursor()) as cur:
        for ddl in WORKFLOW_DDL:
            cur.execute(ddl)
    mysql.commit()
    print("✓ workflow tables ensured in mysql")

    # 2. Migrate workflows.db tables (in FK-safe order).
    if not os.path.exists(workflows_sqlite):
        sys.exit(f"{workflows_sqlite} not found")

    with closing(sqlite3.connect(workflows_sqlite)) as wf_db:
        wf_db.row_factory = sqlite3.Row
        for table in ("workflows", "workflow_executions", "workflow_traces", "workflow_journal"):
            copy_table(wf_db, mysql, table)

    mysql.close()
    print("✓ done")


def copy_table(sqlite_db: sqlite3.Connection, mysql, table: str) -> None:
    probe = sqlite_db.execute(
        "SELECT name FROM sqlite_master WHERE type='table' AND name=?",
        (table,),
    ).fetchone()
    if not probe:
        print(f"  · {table}: not present in sqlite — skip")
        return

    rows = sqlite_db.execute(f'SELECT * FROM "{table}"').fetchall()
    if not rows:
        print(f"  · {table}: 0 rows in sqlite — skip")
        return

    cols = rows[0].keys()
    placeholders = ", ".join(["%s"] * len(cols))
    # `error` is a reserved word — quote column names with backticks.
    col_list = ", ".join(f"`{c}`" for c in cols)
    sql = f"INSERT IGNORE INTO `{table}` ({col_list}) VALUES ({placeholders})"

    inserted = 0
    skipped = 0
    with closing(mysql.cursor()) as cur:
        for row in rows:
            values = [_coerce(v) for v in tuple(row)]
            try:
                cur.execute(sql, values)
            except pymysql.err.Error as exc:
                # Surface the offending row so the operator can fix / skip.
                print(
                    f"  ! {table}: failed to insert row "
                    f"{dict(zip(cols, tuple(row)))!r}: {exc}",
                    file=sys.stderr,
                )
                raise
            if cur.rowcount:
                inserted += 1
            else:
                skipped += 1
    mysql.commit()
    print(f"  · {table}: {inserted} inserted, {skipped} skipped (dup)")


# ---------------------------------------------------------------------------
# Date coercion
# ---------------------------------------------------------------------------
# The Python code writes ISO timestamps from datetime.now(timezone.utc).isoformat(),
# which produce strings like:
#     '2026-07-16T18:17:53.596789+00:00'
# SQLite's datetime('now') produces strings like:
#     '2026-07-16 18:17:53'
# Both need to land in MySQL DATETIME(3) which expects:
#     '2026-07-16 18:17:53.596'
# We normalise to that form.

_ISO_DATE = re.compile(
    r"^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})?$"
)


def _coerce(v):
    """Convert ISO date strings to MySQL DATETIME(3); pass everything else
    through unchanged."""
    if isinstance(v, str) and _ISO_DATE.match(v):
        return _iso_to_mysql_datetime(v)
    return v


def _iso_to_mysql_datetime(s: str) -> str:
    # '2026-07-16T18:17:53.596789+00:00' → '2026-07-16 18:17:53.596'
    # Strip timezone suffix; we treat all workflow timestamps as UTC and
    # MySQL's DATETIME(3) column stores them without a zone.
    s = s.rstrip("Z")
    if "+" in s[10:]:  # strip +HH:MM suffix (keep sign detection past the date)
        s = s[: s.rindex("+")]
    elif s.count("-") > 2:  # strip -HH:MM suffix (but not the date's '-')
        # Find the last '-' that's after the time portion
        time_start = s.find("T") if "T" in s else s.find(" ")
        if time_start >= 0 and "-" in s[time_start + 1 :]:
            s = s[: s.rindex("-")]
    s = s.replace("T", " ")
    if "." in s:
        base, frac = s.split(".")
        frac = (frac + "000")[:3]
        s = f"{base}.{frac}"
    return s


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------

def main() -> int:
    workflows_sqlite = os.environ.get(
        "WORKFLOWS_SQLITE",
        "./data/workflows.db",
    )
    database_url = os.environ.get(
        "DATABASE_URL",
        "mysql+aiomysql://managed:managedAgent123@124.221.28.203:3306/managed_agent",
    )
    if not os.path.exists(workflows_sqlite):
        sys.exit(
            f"{workflows_sqlite} not found — run from the repo root that "
            "owns this workflows.db (or set WORKFLOWS_SQLITE)"
        )
    migrate(workflows_sqlite, database_url)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
