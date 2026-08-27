#!/usr/bin/env python3
"""
One-shot migration: auth-sidecar SQLite → MySQL.

Reads:
  ./data/auth.db   — Better Auth tables: user, session, account, verification
  ./data/oma.db    — OMA tenant tables: tenant, membership

Writes into the MySQL database pointed to by DATABASE_URL (same one that
meta-harness uses). Tables are created if missing; existing rows are skipped
via INSERT IGNORE so the script is safe to re-run.

Usage:
    pip install pymysql  # one-off
    python3 migrate-auth-sqlite-to-mysql.py
    # or override paths / url:
    AUTH_SQLITE=./data/auth.db OMA_SQLITE=./data/oma.db \
    DATABASE_URL=mysql+aiomysql://user:pass@host/db \
    python3 migrate-auth-sqlite-to-mysql.py
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
# DATABASE_URL parsing — accepts mysql[+driver]://... or user:pass@tcp(host)/db
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
            "user": m.group(1),
            "password": m.group(2),
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
# MySQL DDL for Better Auth tables (mirror better-auth's auto-migration
# schema so we don't depend on it running at startup).
# ---------------------------------------------------------------------------

BETTER_AUTH_DDL = [
    # better-auth's MySQL adapter writes datetime strings (e.g.
    # "2026-07-23 18:17:53.596"), so date columns MUST be DATETIME(3)
    # (or TIMESTAMP(3)), NOT BIGINT — the adapter doesn't convert.
    """
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
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
    """,
    """
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
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
    """,
    """
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
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
    """,
    """
    CREATE TABLE IF NOT EXISTS `verification` (
      `id` VARCHAR(64) NOT NULL,
      `identifier` VARCHAR(255) NOT NULL,
      `value` TEXT NOT NULL,
      `expiresAt` DATETIME(3) NOT NULL,
      `createdAt` DATETIME(3),
      `updatedAt` DATETIME(3),
      PRIMARY KEY (`id`),
      KEY `verification_identifier_idx` (`identifier`)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
    """,
]


# ---------------------------------------------------------------------------
# Migration
# ---------------------------------------------------------------------------

def migrate(auth_sqlite: str, oma_sqlite: str, database_url: str) -> None:
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

    # 1. Create Better Auth tables (no-op if already present).
    with closing(mysql.cursor()) as cur:
        for ddl in BETTER_AUTH_DDL:
            cur.execute(ddl)
    mysql.commit()
    print("✓ better-auth tables ensured in mysql")

    # 2. Migrate auth.db tables.
    if os.path.exists(auth_sqlite):
        with closing(sqlite3.connect(auth_sqlite)) as auth_db:
            auth_db.row_factory = sqlite3.Row
            for table in ("user", "session", "account", "verification"):
                copy_table(auth_db, mysql, table)
    else:
        print(f"! {auth_sqlite} not found — skipping auth.db migration")

    # 3. Migrate oma.db tenant/membership (platform already owns these rows;
    #    INSERT IGNORE keeps both sides consistent).
    if os.path.exists(oma_sqlite):
        with closing(sqlite3.connect(oma_sqlite)) as oma_db:
            oma_db.row_factory = sqlite3.Row
            for table in ("tenant", "membership"):
                copy_table(oma_db, mysql, table)
    else:
        print(f"! {oma_sqlite} not found — skipping oma.db migration")

    mysql.close()
    print("✓ done")


def copy_table(sqlite_db: sqlite3.Connection, mysql, table: str) -> None:
    # Check source table exists.
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
    col_list = ", ".join(f"`{c}`" for c in cols)
    sql = f"INSERT IGNORE INTO `{table}` ({col_list}) VALUES ({placeholders})"

    inserted = 0
    skipped = 0
    with closing(mysql.cursor()) as cur:
        for row in rows:
            values = [_coerce(v) for v in tuple(row)]
            cur.execute(sql, values)
            if cur.rowcount:
                inserted += 1
            else:
                skipped += 1
    mysql.commit()
    print(f"  · {table}: {inserted} inserted, {skipped} skipped (dup)")


def _coerce(v):
    """SQLite stores better-auth dates as ISO strings (e.g.
    '2026-07-04T11:04:07.496Z'). MySQL DATETIME(3) accepts the same
    values but without the trailing 'Z' and with a space instead of 'T'.
    Convert on the fly; pass everything else through unchanged."""
    if isinstance(v, str) and _ISO_DATE.match(v):
        return _iso_to_mysql_datetime(v)
    return v


_ISO_DATE = __import__("re").compile(
    r"^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?Z?$"
)


def _iso_to_mysql_datetime(s: str) -> str:
    # '2026-07-04T11:04:07.496Z' → '2026-07-04 11:04:07.496'
    s = s.rstrip("Z").replace("T", " ")
    # Truncate/pad fractional seconds to 3 digits (millisecond precision)
    if "." in s:
        base, frac = s.split(".")
        frac = (frac + "000")[:3]
        s = f"{base}.{frac}"
    return s


# ---------------------------------------------------------------------------
# Entry point
# ---------------------------------------------------------------------------

def main() -> int:
    auth_sqlite = os.environ.get("AUTH_SQLITE", "./data/auth.db")
    oma_sqlite = os.environ.get("OMA_SQLITE", "./data/oma.db")
    database_url = os.environ.get(
        "DATABASE_URL",
        "mysql+aiomysql://managed:managedAgent123@124.221.28.203:3306/managed_agent",
    )
    if not os.path.exists(auth_sqlite) and not os.path.exists(oma_sqlite):
        sys.exit(
            f"neither {auth_sqlite} nor {oma_sqlite} found — "
            "run from the repo root (where ./data/ lives)"
        )
    migrate(auth_sqlite, oma_sqlite, database_url)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
