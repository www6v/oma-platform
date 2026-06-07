package store

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Open opens SQLite at path and applies embedded migrations.
func Open(path string) (*sql.DB, error) {
	dsn := path
	if path == ":memory:" {
		dsn = "file:oma_test?mode=memory&cache=shared"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}
	if err := migrate(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(db *sql.DB) error {
	if _, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			name TEXT PRIMARY KEY,
			applied_at INTEGER NOT NULL
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	names, err := fs.Glob(migrationFiles, "migrations/*.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(names)
	for _, name := range names {
		applied, err := migrationApplied(db, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}
		if shouldBootstrapMigration(db, name) {
			if err := recordMigration(db, name); err != nil {
				return err
			}
			continue
		}
		body, err := migrationFiles.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", name, err)
		}
		if _, err := tx.Exec(string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.Exec(
			`INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`,
			name,
			time.Now().Unix(),
		); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}
	return nil
}

func migrationApplied(db *sql.DB, name string) (bool, error) {
	var applied int
	err := db.QueryRow(
		`SELECT 1 FROM schema_migrations WHERE name = ?`,
		name,
	).Scan(&applied)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check migration %s: %w", name, err)
	}
	return true, nil
}

func shouldBootstrapMigration(db *sql.DB, name string) bool {
	if !strings.HasSuffix(name, "001_core.sql") {
		return false
	}
	return tableExists(db, "agents")
}

func tableExists(db *sql.DB, table string) bool {
	var name string
	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?`,
		table,
	).Scan(&name)
	return err == nil
}

func recordMigration(db *sql.DB, name string) error {
	_, err := db.Exec(
		`INSERT INTO schema_migrations (name, applied_at) VALUES (?, ?)`,
		name,
		time.Now().Unix(),
	)
	if err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	return nil
}

// Close closes the database handle.
func Close(db *sql.DB) error {
	if db == nil {
		return nil
	}
	return db.Close()
}

// IsUniqueViolation reports whether err is a SQLite unique constraint failure.
func IsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed")
}
