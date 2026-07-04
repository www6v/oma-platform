package session

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"

	"github.com/open-ma/oma-building/internal/fileblob"
	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/store"
)

func TestPrepareDefineOutcomePreservesFileRubric(t *testing.T) {
	raw := json.RawMessage(`{
		"type":"user.define_outcome",
		"description":"Write a summary",
		"rubric":{"type":"file","file_id":"file-rubric-123"}
	}`)
	out, err := PrepareDefineOutcome(raw)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatal(err)
	}
	rubric, ok := body["rubric"].(map[string]any)
	if !ok {
		t.Fatalf("rubric=%v want object", body["rubric"])
	}
	if rubric["type"] != "file" {
		t.Fatalf("rubric.type=%v want file", rubric["type"])
	}
	if rubric["file_id"] != "file-rubric-123" {
		t.Fatalf("file_id=%v", rubric["file_id"])
	}
}

func TestResolveRubricFile(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	files := store.NewFileRepo(db)
	blobs := fileblob.NewStore(t.TempDir())
	tenantID := "default"
	fileID := "file-rubric-test"
	blobKey, err := blobs.Write(tenantID, fileID, []byte("Grade revenue mention."))
	if err != nil {
		t.Fatal(err)
	}
	_, err = files.Insert(ctx, store.CreateFileInput{
		TenantID:     tenantID,
		ID:           fileID,
		Filename:     "rubric.md",
		MediaType:    "text/markdown",
		SizeBytes:    22,
		Downloadable: true,
		BlobKey:      blobKey,
	})
	if err != nil {
		t.Fatal(err)
	}

	res := &harness.ResourceResolver{Files: files, FileBlobs: blobs}
	raw, _ := json.Marshal(map[string]string{
		"type":    "file",
		"file_id": fileID,
	})
	text, err := ResolveRubric(ctx, tenantID, raw, res)
	if err != nil {
		t.Fatal(err)
	}
	if text != "Grade revenue mention." {
		t.Fatalf("text=%q", text)
	}
}

func TestResolveRubricFileMissing(t *testing.T) {
	ctx := context.Background()
	res := &harness.ResourceResolver{
		Files:     store.NewFileRepo(mustOpenMemoryDB(t)),
		FileBlobs: fileblob.NewStore(t.TempDir()),
	}
	raw, _ := json.Marshal(map[string]string{
		"type":    "file",
		"file_id": "file-missing",
	})
	_, err := ResolveRubric(ctx, "default", raw, res)
	if err == nil {
		t.Fatal("expected error for missing rubric file")
	}
}

func mustOpenMemoryDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })
	return db
}
