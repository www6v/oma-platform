package store

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
)

// TestDB wraps a test database handle.
type TestDB struct {
	DB *sql.DB
}

// OpenTestDB opens an in-memory SQLite database for tests.
// Each test gets an isolated database to avoid parallel test races.
func OpenTestDB(t *testing.T) *TestDB {
	t.Helper()
	name := strings.ReplaceAll(t.Name(), "/", "_")
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", name)
	db, err := Open(dsn)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = Close(db) })
	return &TestDB{DB: db}
}
