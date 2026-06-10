package store

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

const expectedMigrationCount = 6

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
	if count != expectedMigrationCount {
		t.Fatalf(
			"expected %d applied migrations, got %d",
			expectedMigrationCount,
			count,
		)
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
	coreSQL, err := migrationFiles.ReadFile("migrations/001_core.sql")
	if err != nil {
		t.Fatalf("read core migration: %v", err)
	}
	if _, err := seed.Exec(string(coreSQL)); err != nil {
		t.Fatalf("seed core schema: %v", err)
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
	if count != expectedMigrationCount {
		t.Fatalf(
			"expected %d bootstrapped migrations, got %d",
			expectedMigrationCount,
			count,
		)
	}

	if !tableExists(db, "environments") {
		t.Fatal("expected environments table after P1 migration")
	}
	var hasEnvCol int
	err = db.QueryRow(`
		SELECT COUNT(*) FROM pragma_table_info('sessions')
		WHERE name = 'environment_id'
	`).Scan(&hasEnvCol)
	if err != nil {
		t.Fatalf("check sessions.environment_id: %v", err)
	}
	if hasEnvCol != 1 {
		t.Fatal("expected sessions.environment_id column")
	}
}
