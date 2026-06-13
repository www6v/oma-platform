package harness_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/open-ma/oma-building/internal/fileblob"
	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/store"
)

func TestResourceResolverFileAndEnv(t *testing.T) {
	ctx := context.Background()
	db := store.OpenTestDB(t)
	files := store.NewFileRepo(db.DB)
	blobs := fileblob.NewStore(t.TempDir())
	blobKey, err := blobs.Write("default", "file_test", []byte("hello file"))
	if err != nil {
		t.Fatal(err)
	}
	row, err := files.Insert(ctx, store.CreateFileInput{
		ID:       "file_test",
		TenantID: "default",
		Filename: "note.txt",
		BlobKey:  blobKey,
	})
	if err != nil || row == nil {
		t.Fatal(err)
	}

	resolver := &harness.ResourceResolver{
		Files:     files,
		FileBlobs: blobs,
	}
	envSnap, _ := json.Marshal(map[string]any{
		"config": map[string]any{
			"resources": []any{
				map[string]any{
					"type":       "file",
					"file_id":    "file_test",
					"mount_path": "/mnt/session/uploads/note.txt",
				},
				map[string]any{
					"type":  "env",
					"name":  "EVAL_FLAG",
					"value": "1",
				},
			},
		},
	})
	got, err := resolver.ResolveForTurn(ctx, "default", envSnap)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("resources len=%d want 2", len(got))
	}
}
