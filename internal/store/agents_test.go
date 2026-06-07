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

func TestCreateAgentStoresToolsAndDescription(t *testing.T) {
	db := openTestDB(t)
	repo := store.NewAgentRepo(db.DB)
	ctx := context.Background()

	created, err := repo.Create(ctx, store.CreateAgentInput{
		Name:        "tools",
		Model:       "claude-sonnet-4-20250514",
		Description: "demo agent",
		Tools:       []byte(`[{"type":"agent_toolset_20260401"}]`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if created.Description != "demo agent" {
		t.Fatalf("description=%q", created.Description)
	}
	if string(created.Tools) != `[{"type":"agent_toolset_20260401"}]` {
		t.Fatalf("tools=%s", created.Tools)
	}
}

func TestListVersionsHistoricalOnly(t *testing.T) {
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

	versions, err := repo.ListVersions(ctx, "default", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 0 {
		t.Fatalf("expected no historical versions, got %d", len(versions))
	}

	newName := "v2"
	if _, err := repo.Update(ctx, "default", created.ID, store.UpdateAgentInput{
		Name: &newName,
	}); err != nil {
		t.Fatal(err)
	}

	versions, err = repo.ListVersions(ctx, "default", created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) != 1 || versions[0].Version != 1 {
		t.Fatalf("versions=%+v", versions)
	}
	if versions[0].Snapshot.Name != "v1" {
		t.Fatalf("snapshot name=%q", versions[0].Snapshot.Name)
	}

	got, err := repo.GetVersion(ctx, "default", created.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Snapshot.Name != "v1" {
		t.Fatalf("got=%+v", got)
	}

	missing, err := repo.GetVersion(ctx, "default", created.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Fatalf("expected nil for current version row, got %+v", missing)
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
