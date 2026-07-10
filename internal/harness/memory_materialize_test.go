package harness_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/memoryblob"
	"github.com/open-ma/oma-building/internal/store"
)

func TestMaterializeMemoryStore(t *testing.T) {
	t.Parallel()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	blobs := memoryblob.NewStore(t.TempDir())
	repo := store.NewMemoryStoreRepo(db, blobs)
	ctx := context.Background()
	row, err := repo.CreateStore(ctx, "default", "user-preferences", nil)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.WriteMemory(
		ctx,
		"default",
		row.ID,
		"/preferences/formatting.md",
		"bullet points",
		"test",
		"sess1",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(t.TempDir(), row.ID)
	if err := harness.MaterializeMemoryStore(
		ctx, "default", row.ID, target, repo,
	); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(
		filepath.Join(target, "preferences", "formatting.md"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "bullet points" {
		t.Fatalf("content=%q", string(got))
	}
}
