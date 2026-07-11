package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/store"
)

func TestWorkspaceBackupRepoRecordAndFind(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	repo := store.NewWorkspaceBackupRepo(db)
	ctx := context.Background()
	handle := store.WorkspaceBackupHandle{
		ID:  "wsb_test",
		Dir: "workspace-backups/default/sess-1/wsb_test.tar",
	}
	if err := repo.Record(ctx, store.RecordWorkspaceBackupInput{
		TenantID:        "default",
		EnvironmentID:   store.DefaultEnvironmentID,
		SourceSessionID: "sess-1",
		Handle:          handle,
		TTL:             time.Hour,
	}); err != nil {
		t.Fatal(err)
	}
	row, err := repo.FindLatest(
		ctx, "default", store.DefaultEnvironmentID, "sess-1",
	)
	if err != nil {
		t.Fatal(err)
	}
	if row == nil {
		t.Fatal("expected backup row")
	}
	if row.Handle.ID != handle.ID {
		t.Fatalf("handle id=%s", row.Handle.ID)
	}
}

func TestWorkspaceBackupRepoPruneExpired(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	repo := store.NewWorkspaceBackupRepo(db)
	ctx := context.Background()
	past := time.Now().Add(-time.Hour).UnixMilli()
	_, err = db.Exec(`
		INSERT INTO workspace_backups (
			tenant_id, environment_id, backup_handle,
			created_at, expires_at, source_session_id
		) VALUES (?, ?, ?, ?, ?, ?)
	`, "default", store.DefaultEnvironmentID,
		`{"id":"old","dir":"workspace-backups/x.tar"}`,
		past, past, "sess-old")
	if err != nil {
		t.Fatal(err)
	}
	n, err := repo.PruneExpired(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("pruned=%d", n)
	}
}
