package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/store"
)

func TestWakeupRepoCreateListDelete(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	repo := store.NewWakeupRepo(db)
	ctx := context.Background()
	now := time.Now()
	row := store.WakeupSchedule{
		ID:        store.NewScheduleID(),
		TenantID:  "tenant-default",
		SessionID: "sess-test",
		Prompt:    "ping",
		Kind:      store.WakeupKindOneShot,
		FireAt:    now.Add(time.Minute).Unix(),
		CreatedAt: now.UnixMilli(),
	}
	if err := repo.Create(ctx, row); err != nil {
		t.Fatal(err)
	}

	count, err := repo.CountPending(ctx, row.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count=%d", count)
	}

	rows, err := repo.ListForSession(ctx, row.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != row.ID {
		t.Fatalf("rows=%v", rows)
	}

	ok, err := repo.Delete(ctx, row.SessionID, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("expected delete ok")
	}
}
