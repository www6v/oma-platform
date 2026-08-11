package store

import (
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
)

// TestDB wraps a test database handle.
type TestDB struct {
	DB *sql.DB
}

// OpenTestDB opens a test database. When OMA_TEST_MYSQL_DSN is set it
// connects to that MySQL instance (schema pre-created from
// scripts/sql/platform_mysql.sql); otherwise it falls back to the legacy
// in-memory SQLite DSN. Each test gets an isolated database to avoid
// parallel test races.
func OpenTestDB(t *testing.T) *TestDB {
	t.Helper()
	if dsn := os.Getenv("OMA_TEST_MYSQL_DSN"); dsn != "" {
		db, err := Open(dsn)
		if err != nil {
			t.Fatalf("open test db (mysql): %v", err)
		}
		t.Cleanup(func() { _ = Close(db) })
		return &TestDB{DB: db}
	}
	name := strings.ReplaceAll(t.Name(), "/", "_")
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", name)
	db, err := Open(dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = Close(db) })
	return &TestDB{DB: db}
}
