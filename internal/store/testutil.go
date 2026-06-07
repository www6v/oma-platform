package store

import (
	"database/sql"
	"testing"
)

// TestDB wraps a test database handle.
type TestDB struct {
	DB *sql.DB
}

// OpenTestDB opens an in-memory SQLite database for tests.
func OpenTestDB(t *testing.T) *TestDB {
	t.Helper()
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = Close(db) })
	return &TestDB{DB: db}
}
