package store_test

import (
	"context"
	"testing"

	"github.com/open-ma/oma-building/internal/store"
)

func openTestDB(t *testing.T) *store.TestDB {
	t.Helper()
	return store.OpenTestDB(t)
}

func TestCreateAgentIncrementsVersion(t *testing.T) {
	db := openTestDB(t)
	repo := store.NewAgentRepo(db.DB)
	ctx := context.Background()
	a, err := repo.Create(ctx, store.CreateAgentInput{
		Name:         "demo",
		Model:        "claude-sonnet-4-20250514",
		SystemPrompt: "hi",
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.Version != 1 {
		t.Fatalf("version=%d", a.Version)
	}
	if a.ID == "" {
		t.Fatal("expected generated id")
	}
}

func TestGetAgent(t *testing.T) {
	db := openTestDB(t)
	repo := store.NewAgentRepo(db.DB)
	ctx := context.Background()

	created, err := repo.Create(ctx, store.CreateAgentInput{
		Name:  "lookup",
		Model: "claude-sonnet-4-20250514",
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := repo.Get(ctx, "default", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Name != "lookup" {
		t.Fatalf("got=%+v", got)
	}
}

func TestListExcludesArchived(t *testing.T) {
	db := openTestDB(t)
	repo := store.NewAgentRepo(db.DB)
	ctx := context.Background()

	active, err := repo.Create(ctx, store.CreateAgentInput{
		Name:  "active",
		Model: "claude-sonnet-4-20250514",
	})
	if err != nil {
		t.Fatal(err)
	}
	archived, err := repo.Create(ctx, store.CreateAgentInput{
		Name:  "gone",
		Model: "claude-sonnet-4-20250514",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Archive(ctx, "default", archived.ID); err != nil {
		t.Fatal(err)
	}

	items, err := repo.List(ctx, "default", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ID != active.ID {
		t.Fatalf("items=%+v", items)
	}
}

func TestUpdateBumpsVersion(t *testing.T) {
	db := openTestDB(t)
	repo := store.NewAgentRepo(db.DB)
	ctx := context.Background()

	created, err := repo.Create(ctx, store.CreateAgentInput{
		Name:  "v1",
		Model: "claude-sonnet-4-20250514",
	})
	if err != nil {
		t.Fatal(err)
	}

	newName := "v2"
	updated, err := repo.Update(ctx, "default", created.ID, store.UpdateAgentInput{
		Name: &newName,
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Version != 2 {
		t.Fatalf("version=%d", updated.Version)
	}
	if updated.Name != "v2" {
		t.Fatalf("name=%q", updated.Name)
	}
}

func TestArchiveSetsArchivedAt(t *testing.T) {
	db := openTestDB(t)
	repo := store.NewAgentRepo(db.DB)
	ctx := context.Background()

	created, err := repo.Create(ctx, store.CreateAgentInput{
		Name:  "bye",
		Model: "claude-sonnet-4-20250514",
	})
	if err != nil {
		t.Fatal(err)
	}

	archived, err := repo.Archive(ctx, "default", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if archived.ArchivedAt == nil {
		t.Fatal("expected archived_at")
	}
}
