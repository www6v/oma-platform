package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestOpenAppliesMigrationsOnce(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "oma.db")

	db1, err := Open(path)
	if err != nil {
		t.Fatalf("first open: %v", err)
	}
	if err := Close(db1); err != nil {
		t.Fatalf("close first db: %v", err)
	}

	db2, err := Open(path)
	if err != nil {
		t.Fatalf("second open: %v", err)
	}
	defer Close(db2)

	var count int
	if err := db2.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 applied migration, got %d", count)
	}
}

func TestOpenBootstrapsExistingDatabase(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "oma.db")

	seed, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	if _, err := seed.Exec(`CREATE TABLE agents (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatalf("seed agents table: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	db, err := Open(path)
	if err != nil {
		t.Fatalf("bootstrap open: %v", err)
	}
	defer Close(db)

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&count); err != nil {
		t.Fatalf("count migrations: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected bootstrapped migration, got %d", count)
	}
}
