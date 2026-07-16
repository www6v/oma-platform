package store

import (
	"database/sql"
	// [SQLite] 原驱动（已切换到 MySQL）
	// _ "modernc.org/sqlite"
	// [MySQL] 新驱动
	_ "github.com/go-sql-driver/mysql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// ===========================================================================
// [SQLite] 原 Open 函数（已注释；切换到 MySQL 后由 OpenMySQL 替代）
// ===========================================================================
//
// // Open opens SQLite at path and applies embedded migrations.
// func Open(path string) (*sql.DB, error) {
// 	dsn := path
// 	if path == ":memory:" {
// 		dsn = "file:oma_test?mode=memory&cache=shared"
// 	}
// 	db, err := sql.Open("sqlite", dsn)
// 	if err != nil {
// 		return nil, fmt.Errorf("open sqlite: %w", err)
// 	}
// 	if err := db.Ping(); err != nil {
// 		_ = db.Close()
// 		return nil, fmt.Errorf("ping sqlite: %w", err)
// 	}
// 	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
// 		_ = db.Close()
// 		return nil, fmt.Errorf("enable foreign keys: %w", err)
// 	}
// 	if _, err := db.Exec(`PRAGMA journal_mode = WAL`); err != nil {
// 		_ = db.Close()
// 		return nil, fmt.Errorf("enable wal: %w", err)
// 	}
// 	if _, err := db.Exec(`PRAGMA busy_timeout = 10000`); err != nil {
// 		_ = db.Close()
// 		return nil, fmt.Errorf("set busy timeout: %w", err)
// 	}
// 	if err := migrate(db); err != nil {
// 		_ = db.Close()
// 		return nil, err
// 	}
// 	return db, nil
// }

// ===========================================================================
// [MySQL] 新 Open 函数 — 连接 MySQL，表已预先通过 platform_mysql.sql 创建
// ===========================================================================

// Open 连接 MySQL。dsn 格式示例：
//
//	managed:managedAgent123@tcp(124.221.28.203:3306)/managed_agent?parseTime=true&charset=utf8mb4
//
// 也可传入 DATABASE_URL 风格的 URL（mysql://... 或 mysql+aiomysql://...），
// 函数内部会自动转换为 Go MySQL driver DSN。
func Open(dsn string) (*sql.DB, error) {
	goDSN, err := normalizeMySQLDSN(dsn)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open("mysql", goDSN)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}
	// 连接池调优
	db.SetMaxOpenConns(50)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}
	// 确保使用 utf8mb4
	if _, err := db.Exec(`SET NAMES utf8mb4`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("set names utf8mb4: %w", err)
	}
	return db, nil
}

// normalizeMySQLDSN 把多种格式的 MySQL 连接串统一为 Go driver DSN。
// 支持：
//   - 原生 Go DSN: user:pass@tcp(host:port)/db?params
//   - URL 格式:    mysql://user:pass@host:port/db
//   - URL 格式:    mysql+aiomysql://user:pass@host:port/db
func normalizeMySQLDSN(dsn string) (string, error) {
	// 已是 Go DSN 格式（含 @tcp(）
	if strings.Contains(dsn, "@tcp(") {
		return dsn, nil
	}
	// URL 格式: mysql[+driver]://user:pass@host:port/db[?params]
	s := dsn
	for _, prefix := range []string{"mysql+aiomysql://", "mysql+mysqlconnector://", "mysql://"} {
		if strings.HasPrefix(s, prefix) {
			s = strings.TrimPrefix(s, prefix)
			break
		}
	}
	// 此时 s = "user:pass@host:port/db?params"
	atIdx := strings.LastIndex(s, "@")
	if atIdx < 0 {
		return "", fmt.Errorf("invalid mysql DSN: missing @: %q", dsn)
	}
	userPass := s[:atIdx]
	rest := s[atIdx+1:] // "host:port/db?params"

	slashIdx := strings.Index(rest, "/")
	if slashIdx < 0 {
		return "", fmt.Errorf("invalid mysql DSN: missing /db: %q", dsn)
	}
	hostPort := rest[:slashIdx]
	dbAndParams := rest[slashIdx:] // "/db?params"

	// 拼装 Go DSN: user:pass@tcp(host:port)/db?params
	goDSN := fmt.Sprintf("%s@tcp(%s)%s", userPass, hostPort, dbAndParams)
	// 确保常用参数
	if !strings.Contains(goDSN, "parseTime") {
		if strings.Contains(goDSN, "?") {
			goDSN += "&parseTime=true"
		} else {
			goDSN += "?parseTime=true"
		}
	}
	if !strings.Contains(goDSN, "charset") {
		goDSN += "&charset=utf8mb4"
	}
	return goDSN, nil
}

// ===========================================================================
// [SQLite] 原 migrate 相关函数（已注释；MySQL 表由 platform_mysql.sql 预建）
// ===========================================================================
//
// func migrate(db *sql.DB) error {
// 	if _, err := db.Exec(`
// 		CREATE TABLE IF NOT EXISTS schema_migrations (
// 			name TEXT PRIMARY KEY,
// 			applied_at INTEGER NOT NULL
// 		)
// 	`); err != nil {
// 		return fmt.Errorf("create schema_migrations: %w", err)
// 	}
//
// 	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
// 	if err != nil {
// 		return fmt.Errorf("list migrations: %w", err)
// 	}
// 	sort.Strings(names)
// 	for _, name := range names {
// 		applied, err := migrationApplied(db, name)
// 		if err != nil {
// 			return err
// 		}
// 		if applied {
// 			continue
// 		}
// 		if shouldBootstrapMigration(db, name) {
// 			if err := recordMigration(db, name); err != nil {
// 				return err
// 			}
// 			continue
// 		}
// 		body, err := migrationFiles.ReadFile(name)
// 		if err != nil {
// 			return fmt.Errorf("read migration %s: %w", name, err)
// 		}
// 		tx, err := db.Begin()
// 		if err != nil {
// 			return fmt.Errorf("begin migration %s: %w", name, err)
// 		}
// 		if _, err := tx.Exec(string(body)); err != nil {
// 			_ = tx.Rollback()
// 			if shouldBootstrapMigration(db, name) {
// 				if err := recordMigration(db, name); err != nil {
// 					return err
// 				}
// 				continue
// 			}
// 			return fmt.Errorf("apply migration %s: %w", name, err)
// 		}
// 		if _, err := tx.Exec(
// 			`INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`,
// 			name,
// 			time.Now().Unix(),
// 		); err != nil {
// 			_ = tx.Rollback()
// 			return fmt.Errorf("record migration %s: %w", name, err)
// 		}
// 		if err := tx.Commit(); err != nil {
// 			return fmt.Errorf("commit migration %s: %w", name, err)
// 		}
// 	}
// 	return nil
// }
//
// func migrationApplied(db *sql.DB, name string) (bool, error) {
// 	var applied int
// 	err := db.QueryRow(
// 		`SELECT 1 FROM schema_migrations WHERE name = ?`,
// 		name,
// 	).Scan(&applied)
// 	if err == sql.ErrNoRows {
// 		return false, nil
// 	}
// 	if err != nil {
// 		return false, fmt.Errorf("check migration %s: %w", name, err)
// 	}
// 	return true, nil
// }
//
// func shouldBootstrapMigration(db *sql.DB, name string) bool {
// 	if !strings.HasSuffix(name, "001_core.sql") {
// 		return false
// 	}
// 	return tableExists(db, "agents")
// }

// ===========================================================================
// [SQLite] 原 tableExists（已注释）
// ===========================================================================
//
// func tableExists(db *sql.DB, table string) bool {
// 	var name string
// 	err := db.QueryRow(
// 		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`,
// 		table,
// 	).Scan(&name)
// 	return err == nil
// }

// [MySQL] 等价实现
func tableExists(db *sql.DB, table string) bool {
	var name string
	err := db.QueryRow(`SHOW TABLES LIKE ?`, table).Scan(&name)
	return err == nil
}

// ===========================================================================
// [SQLite] 原 recordMigration（已注释）
// ===========================================================================
//
// func recordMigration(db *sql.DB, name string) error {
// 	_, err := db.Exec(
// 		`INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`,
// 		name,
// 		time.Now().Unix(),
// 	)
// 	if err != nil {
// 		return fmt.Errorf("record migration %s: %w", name, err)
// 	}
// 	return nil
// }

// Close closes the database handle.
func Close(db *sql.DB) error {
	if db == nil {
		return nil
	}
	return db.Close()
}

// ===========================================================================
// [SQLite] 原 IsUniqueViolation（已注释）
// ===========================================================================
//
// // IsUniqueViolation reports whether err is a SQLite unique constraint failure.
// func IsUniqueViolation(err error) bool {
// 	if err == nil {
// 		return false
// 	}
// 	msg := err.Error()
// 	return strings.Contains(msg, "UNIQUE constraint failed")
// }

// [MySQL] 等价实现 — 检测 MySQL 唯一键冲突（Error 1062）
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	// MySQL Error 1062: "Duplicate entry '...' for key '...'"
	return strings.Contains(msg, "Error 1062") ||
		strings.Contains(msg, "Duplicate entry")
}

// 以下变量保留供将来恢复 SQLite 迁移时使用，避免 import 报错
var (
	_ = embed.FS{}
	_ = fs.Glob
	_ = sort.Strings
	_ = time.Now
)
