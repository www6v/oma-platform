package store_test

import (
	"context"
	"testing"

	"github.com/open-ma/oma-building/internal/store"
)

func TestTenantRepoCreateTenant(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	repo := store.NewTenantRepo(db)
	ctx := context.Background()

	created, err := repo.CreateTenant(ctx, "user_a", "My Workspace")
	if err != nil {
		t.Fatal(err)
	}
	if created.TenantID == "" || created.Name != "My Workspace" {
		t.Fatalf("created=%+v", created)
	}
	if created.Role != "owner" {
		t.Fatalf("role=%s", created.Role)
	}

	items, err := repo.ListForUser(ctx, "user_a")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].TenantID != created.TenantID {
		t.Fatalf("items=%v", items)
	}
}
