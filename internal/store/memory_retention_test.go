package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/store"
)

func TestPruneVersionsOlderThanKeepsLatest(t *testing.T) {
	tdb := store.OpenTestDB(t)
	repo := store.NewMemoryStoreRepo(tdb.DB, nil)
	ctx := context.Background()

	created, err := repo.CreateStore(ctx, "default", "retention-store", nil)
	if err != nil {
		t.Fatal(err)
	}
	mem, err := repo.WriteMemory(
		ctx, "default", created.ID, "/doc.txt", "v1", "user", "u1", nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	old := time.Now().Add(-40 * 24 * time.Hour).UnixMilli()
	_, err = tdb.DB.ExecContext(ctx, `
		UPDATE memory_versions SET created_at = ? WHERE memory_id = ?
	`, old, mem.ID)
	if err != nil {
		t.Fatal(err)
	}

	_, err = repo.WriteMemory(
		ctx, "default", created.ID, "/doc.txt", "v2", "user", "u1", nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	cutoff := time.Now().Add(-30 * 24 * time.Hour).UnixMilli()
	removed, err := repo.PruneVersionsOlderThan(ctx, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 removed version, got %d", removed)
	}

	versions, err := repo.ListVersions(ctx, "default", created.ID, mem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 {
		t.Fatalf("expected 1 version left, got %d", len(versions))
	}
	if !versions[0].Content.Valid || versions[0].Content.String != "v2" {
		t.Fatalf("expected latest version content v2, got %v", versions[0].Content)
	}
}
