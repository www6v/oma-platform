package store_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/store"
)

// These tests run against MySQL when OMA_TEST_MYSQL_DSN is set (see
// OpenTestDB). They use unique tenant/store ids and clean up after
// themselves so shared dev databases stay tidy.

func TestEnsureStoreWithIDIdempotent(t *testing.T) {
	tdb := store.OpenTestDB(t)
	repo := store.NewMemoryStoreRepo(tdb.DB, nil)
	ctx := context.Background()

	tenant := fmt.Sprintf("t-memtest-%d", time.Now().UnixNano())
	storeID := "agentmem-memtest-agent-1"
	t.Cleanup(func() {
		_ = repo.DeleteStore(context.Background(), tenant, storeID)
	})

	first, err := repo.EnsureStoreWithID(ctx, tenant, storeID, "Agent Memory", "agent_builtin")
	if err != nil {
		t.Fatal(err)
	}
	if first == nil || first.ID != storeID || first.Kind != "agent_builtin" {
		t.Fatalf("unexpected first store: %+v", first)
	}

	second, err := repo.EnsureStoreWithID(ctx, tenant, storeID, "Agent Memory", "agent_builtin")
	if err != nil {
		t.Fatal(err)
	}
	if second == nil || second.ID != storeID {
		t.Fatalf("unexpected second store: %+v", second)
	}
	if second.CreatedAt != first.CreatedAt {
		t.Fatalf("EnsureStoreWithID not idempotent: created %d vs %d",
			first.CreatedAt, second.CreatedAt)
	}
}

func TestListStoresFiltersBuiltin(t *testing.T) {
	tdb := store.OpenTestDB(t)
	repo := store.NewMemoryStoreRepo(tdb.DB, nil)
	ctx := context.Background()

	tenant := fmt.Sprintf("t-memtest-%d", time.Now().UnixNano())
	builtinID := "agentmem-memtest-agent-2"

	standard, err := repo.CreateStore(ctx, tenant, "standard-store", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = repo.DeleteStore(context.Background(), tenant, standard.ID)
		_ = repo.DeleteStore(context.Background(), tenant, builtinID)
	})
	builtin, err := repo.EnsureStoreWithID(ctx, tenant, builtinID, "Agent Memory", "agent_builtin")
	if err != nil || builtin == nil {
		t.Fatalf("ensure builtin store: %v %+v", err, builtin)
	}

	listed, err := repo.ListStores(ctx, tenant, store.MemoryStoreListOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].ID != standard.ID {
		t.Fatalf("default list should hide builtin stores, got %+v", listed)
	}

	all, err := repo.ListStores(ctx, tenant, store.MemoryStoreListOptions{IncludeBuiltin: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("include_builtin should return both stores, got %+v", all)
	}
}

func TestGetMemoryByPathHydrates(t *testing.T) {
	tdb := store.OpenTestDB(t)
	repo := store.NewMemoryStoreRepo(tdb.DB, nil)
	ctx := context.Background()

	tenant := fmt.Sprintf("t-memtest-%d", time.Now().UnixNano())
	storeID := "agentmem-memtest-agent-3"
	created, err := repo.EnsureStoreWithID(ctx, tenant, storeID, "Agent Memory", "agent_builtin")
	if err != nil || created == nil {
		t.Fatalf("ensure store: %v %+v", err, created)
	}
	t.Cleanup(func() {
		_ = repo.DeleteStore(context.Background(), tenant, storeID)
	})

	if _, err := repo.WriteMemory(ctx, tenant, storeID, "/MEMORY.md", "entry-a\n§\nentry-b", "agent_session", "sess-1", nil); err != nil {
		t.Fatal(err)
	}

	mem, err := repo.GetMemoryByPath(ctx, tenant, storeID, "/MEMORY.md")
	if err != nil {
		t.Fatal(err)
	}
	if mem == nil || mem.Content != "entry-a\n§\nentry-b" {
		t.Fatalf("unexpected memory: %+v", mem)
	}

	missing, err := repo.GetMemoryByPath(ctx, tenant, storeID, "/USER.md")
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Fatalf("expected nil for missing path, got %+v", missing)
	}
}
