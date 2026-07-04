package harness_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/memoryblob"
	"github.com/open-ma/oma-building/internal/store"
)

func TestSyncMemoryStoresFromWorkdir(t *testing.T) {
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

	workdir := t.TempDir()
	memFile := filepath.Join(
		workdir, "mnt", "memory", "user-preferences",
		"preferences", "formatting.md",
	)
	if err := os.MkdirAll(filepath.Dir(memFile), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "User prefers bullet points."
	if err := os.WriteFile(memFile, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	raw, _ := json.Marshal(map[string]any{
		"type":       "memory_store",
		"store_id":   row.ID,
		"store_name": row.Name,
		"read_only":  false,
	})
	bindings := harness.MemoryStoreBindings([]json.RawMessage{raw})
	if err := harness.SyncMemoryStoresFromWorkdir(
		ctx, workdir, "default", "sess_mem",
		bindings, repo,
	); err != nil {
		t.Fatal(err)
	}

	rows, err := repo.ListMemories(ctx, "default", row.ID, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("memories=%d want 1", len(rows))
	}
	got, err := repo.GetMemory(ctx, "default", row.ID, rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != content {
		t.Fatalf("content=%q want %q", got.Content, content)
	}
}

func TestMemoryContentAtPath(t *testing.T) {
	t.Parallel()
	raw, _ := json.Marshal(map[string]any{
		"type": "memory_store",
		"memories": []map[string]string{
			{
				"path":    "/preferences/formatting.md",
				"content": "bullet points",
			},
		},
	})
	text, ok := harness.MemoryContentAtPath(
		[]json.RawMessage{raw}, "/preferences/formatting.md",
	)
	if !ok || text != "bullet points" {
		t.Fatalf("got=%q ok=%v", text, ok)
	}
}
