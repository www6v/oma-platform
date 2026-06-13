package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/open-ma/oma-building/internal/memoryblob"
	"github.com/open-ma/oma-building/internal/store"
)

func TestMemoryBlobOffloadAndHydrate(t *testing.T) {
	tdb := store.OpenTestDB(t)
	blobs := memoryblob.NewStore(t.TempDir())
	repo := store.NewMemoryStoreRepo(tdb.DB, blobs)
	ctx := context.Background()

	created, err := repo.CreateStore(ctx, "default", "blob-store", nil)
	if err != nil {
		t.Fatal(err)
	}

	large := strings.Repeat("x", 8*1024)
	mem, err := repo.WriteMemory(
		ctx, "default", created.ID, "/large.txt", large, "user", "u1", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if mem.BlobKey == "" {
		t.Fatal("expected blob_key for large content")
	}
	if mem.Content != large {
		t.Fatal("WriteMemory should return hydrated content")
	}

	row := tdb.DB.QueryRow(`
		SELECT content, blob_key FROM memories WHERE id = ?
	`, mem.ID)
	var inline string
	var blobKey *string
	if err := row.Scan(&inline, &blobKey); err != nil {
		t.Fatal(err)
	}
	if inline != "" {
		t.Fatalf("expected empty inline content, got len=%d", len(inline))
	}
	if blobKey == nil || *blobKey == "" {
		t.Fatal("expected blob_key in database")
	}

	got, err := repo.GetMemory(ctx, "default", created.ID, mem.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != large {
		t.Fatalf("GetMemory content mismatch len=%d", len(got.Content))
	}
}

func TestMemoryInlineWhenSmall(t *testing.T) {
	tdb := store.OpenTestDB(t)
	blobs := memoryblob.NewStore(t.TempDir())
	repo := store.NewMemoryStoreRepo(tdb.DB, blobs)
	ctx := context.Background()

	created, err := repo.CreateStore(ctx, "default", "inline-store", nil)
	if err != nil {
		t.Fatal(err)
	}

	small := "hello"
	mem, err := repo.WriteMemory(
		ctx, "default", created.ID, "/small.txt", small, "user", "u1", nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if mem.BlobKey != "" {
		t.Fatal("small content should stay inline")
	}
	if mem.Content != small {
		t.Fatal("content mismatch")
	}
}
