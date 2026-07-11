package workdir_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-ma/oma-building/internal/fileblob"
	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/workdir"
)

func TestBackupSnapshotAndRestore(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	repo := store.NewWorkspaceBackupRepo(db)
	blobs := fileblob.NewStore(t.TempDir())
	svc := workdir.NewBackupService(repo, blobs)
	base := t.TempDir()
	sessionID := "sess-backup-1"
	workdirPath := filepath.Join(base, sessionID)
	if err := os.MkdirAll(workdirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	hello := filepath.Join(workdirPath, "hello.txt")
	if err := os.WriteFile(hello, []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(workdirPath, "sub", "deep.txt")
	if err := os.MkdirAll(filepath.Dir(nested), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nested, []byte("nested"), 0o644); err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := svc.Snapshot(
		ctx, "default", store.DefaultEnvironmentID, sessionID, workdirPath,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(workdirPath); err != nil {
		t.Fatal(err)
	}
	if err := svc.TryRestore(
		ctx, "default", store.DefaultEnvironmentID, sessionID, workdirPath,
	); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(workdirPath, "hello.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "world" {
		t.Fatalf("hello=%q", string(data))
	}
	deep, err := os.ReadFile(filepath.Join(workdirPath, "sub", "deep.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(deep) != "nested" {
		t.Fatalf("deep=%q", string(deep))
	}
}

func TestBackupTryRestoreSkipsNonEmptyWorkdir(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	repo := store.NewWorkspaceBackupRepo(db)
	blobs := fileblob.NewStore(t.TempDir())
	svc := workdir.NewBackupService(repo, blobs)
	base := t.TempDir()
	sessionID := "sess-backup-2"
	workdirPath := filepath.Join(base, sessionID)
	if err := os.MkdirAll(workdirPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(workdirPath, "seed.txt"),
		[]byte("keep"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := svc.Snapshot(
		ctx, "default", store.DefaultEnvironmentID, sessionID, workdirPath,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(workdirPath, "seed.txt"),
		[]byte("changed"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := svc.TryRestore(
		ctx, "default", store.DefaultEnvironmentID, sessionID, workdirPath,
	); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(workdirPath, "seed.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "changed" {
		t.Fatalf("seed=%q", string(data))
	}
}
