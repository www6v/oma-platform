package store_test

import (
	"context"
	"testing"

	"github.com/open-ma/oma-building/internal/store"
)

func TestEnsureDefaultEnvironment(t *testing.T) {
	db := openTestDB(t)
	repo := store.NewEnvironmentRepo(db.DB)
	ctx := context.Background()

	if err := repo.EnsureDefault(ctx); err != nil {
		t.Fatal(err)
	}
	env, err := repo.Get(ctx, "default", store.DefaultEnvironmentID)
	if err != nil {
		t.Fatal(err)
	}
	if env == nil || env.Name != "local-default" {
		t.Fatalf("env=%+v", env)
	}
}

func TestCreateAndArchiveEnvironment(t *testing.T) {
	db := openTestDB(t)
	repo := store.NewEnvironmentRepo(db.DB)
	ctx := context.Background()

	created, err := repo.Create(ctx, store.CreateEnvironmentInput{
		Name: "cloud-dev",
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
