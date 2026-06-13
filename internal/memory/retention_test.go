package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/memory"
	"github.com/open-ma/oma-building/internal/store"
)

func TestRetentionWorkerRunOnce(t *testing.T) {
	tdb := store.OpenTestDB(t)
	repo := store.NewMemoryStoreRepo(tdb.DB, nil)
	ctx := context.Background()

	created, err := repo.CreateStore(ctx, "default", "cron-store", nil)
	if err != nil {
		t.Fatal(err)
	}
	mem, err := repo.WriteMemory(
		ctx, "default", created.ID, "/a.txt", "v1", "user", "u1", nil,
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
		ctx, "default", created.ID, "/a.txt", "v2", "user", "u1", nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	worker := &memory.RetentionWorker{
		MemoryStores: repo,
		Now: func() time.Time {
			return time.Date(2026, 6, 11, 3, 0, 0, 0, time.UTC)
		},
	}
	removed, err := worker.RunOnce(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if removed != 1 {
		t.Fatalf("expected 1 pruned row, got %d", removed)
	}
}

func TestRetentionWorkerTickGate(t *testing.T) {
	tdb := store.OpenTestDB(t)
	repo := store.NewMemoryStoreRepo(tdb.DB, nil)
	worker := &memory.RetentionWorker{
		MemoryStores: repo,
		Now: func() time.Time {
			return time.Date(2026, 6, 11, 12, 0, 0, 0, time.UTC)
		},
	}
	result, err := worker.Tick(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if result.Ran {
		t.Fatal("Tick should skip outside sweep window")
	}
}
