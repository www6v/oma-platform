package harness_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/open-ma/oma-building/internal/fileblob"
	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/store"
)

func TestScopeSessionResourcesFileCopy(t *testing.T) {
	ctx := context.Background()
	db := store.OpenTestDB(t)
	files := store.NewFileRepo(db.DB)
	blobs := fileblob.NewStore(t.TempDir())
	blobKey, err := blobs.Write("default", "file_src", []byte("scoped content"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = files.Insert(ctx, store.CreateFileInput{
		ID:       "file_src",
		TenantID: "default",
		Filename: "init-file.txt",
		BlobKey:  blobKey,
	})
	if err != nil {
		t.Fatal(err)
	}

	resolver := &harness.ResourceResolver{
		Files:     files,
		FileBlobs: blobs,
	}
	scoped := resolver.ScopeSessionResources(ctx, "default", "sess_test", []map[string]any{
		{
			"type":       "file",
			"file_id":    "file_src",
			"mount_path": "/workspace/init-file.txt",
		},
	})
	if len(scoped) != 1 {
		t.Fatalf("scoped len=%d want 1", len(scoped))
	}
	scopedID, _ := scoped[0]["file_id"].(string)
	if scopedID == "" || scopedID == "file_src" {
		t.Fatalf("expected new scoped file_id, got %q", scopedID)
	}
	row, err := files.Get(ctx, "default", scopedID)
	if err != nil || row == nil {
		t.Fatal("scoped file row missing")
	}
	if !row.SessionID.Valid || row.SessionID.String != "sess_test" {
		t.Fatalf("scope session_id=%v want sess_test", row.SessionID)
	}
}

func TestScopeSessionResourcesSkipsMissingFile(t *testing.T) {
	ctx := context.Background()
	db := store.OpenTestDB(t)
	resolver := &harness.ResourceResolver{
		Files: store.NewFileRepo(db.DB),
	}
	scoped := resolver.ScopeSessionResources(ctx, "default", "sess_test", []map[string]any{
		{"type": "file", "file_id": "file_missing"},
	})
	if len(scoped) != 0 {
		t.Fatalf("expected empty scoped resources, got %d", len(scoped))
	}
}

func TestResolveForTurnMergesSessionOverEnv(t *testing.T) {
	ctx := context.Background()
	db := store.OpenTestDB(t)
	files := store.NewFileRepo(db.DB)
	blobs := fileblob.NewStore(t.TempDir())
	blobKey, err := blobs.Write("default", "file_env", []byte("env"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = files.Insert(ctx, store.CreateFileInput{
		ID: "file_env", TenantID: "default", Filename: "env.txt", BlobKey: blobKey,
	})
	if err != nil {
		t.Fatal(err)
	}
	sessKey, err := blobs.Write("default", "file_sess", []byte("session"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = files.Insert(ctx, store.CreateFileInput{
		ID: "file_sess", TenantID: "default", Filename: "sess.txt", BlobKey: sessKey,
	})
	if err != nil {
		t.Fatal(err)
	}

	resolver := &harness.ResourceResolver{Files: files, FileBlobs: blobs}
	envSnap, _ := json.Marshal(map[string]any{
		"config": map[string]any{
			"resources": []any{
				map[string]any{
					"type":       "file",
					"file_id":    "file_env",
					"mount_path": "/workspace/data.txt",
				},
				map[string]any{
					"type":  "env",
					"name":  "FROM_ENV",
					"value": "1",
				},
			},
		},
	})
	sessionRes, _ := json.Marshal([]any{
		map[string]any{
			"type":       "file",
			"file_id":    "file_sess",
			"mount_path": "/workspace/data.txt",
		},
	})
	got, err := resolver.ResolveForTurn(ctx, "default", envSnap, sessionRes)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("resources len=%d want 2", len(got))
	}
	var filePayload map[string]any
	for _, raw := range got {
		var item map[string]any
		if err := json.Unmarshal(raw, &item); err != nil {
			t.Fatal(err)
		}
		if item["type"] == "file" {
			filePayload = item
		}
	}
	if filePayload == nil {
		t.Fatal("missing file resource")
	}
	if filePayload["file_id"] != "file_sess" {
		t.Fatalf("session file should win on mount_path conflict, got %v", filePayload["file_id"])
	}
}
