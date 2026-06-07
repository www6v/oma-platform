package modelresolve_test

import (
	"context"
	"testing"

	"github.com/open-ma/oma-building/internal/modelresolve"
	"github.com/open-ma/oma-building/internal/store"
)

func TestResolveByModelID(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	cards := store.NewModelCardRepo(db)
	ctx := context.Background()
	if _, err := cards.Create(ctx, store.CreateModelCardInput{
		ModelID:  "my-claude",
		Model:    "claude-sonnet-4-20250514",
		Provider: "ant",
		APIKey:   "secret-key",
	}); err != nil {
		t.Fatal(err)
	}

	resolver := &modelresolve.Resolver{Cards: cards}
	cfg, err := resolver.Resolve(ctx, "default", "my-claude")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "claude-sonnet-4-20250514" || cfg.APIKey != "secret-key" {
		t.Fatalf("cfg=%+v", cfg)
	}
}

func TestResolveUsesDefaultCardForProviderModel(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	cards := store.NewModelCardRepo(db)
	ctx := context.Background()
	if _, err := cards.Create(ctx, store.CreateModelCardInput{
		ModelID:     "default-card",
		Provider:    "ant",
		APIKey:      "sk-default",
		MakeDefault: true,
	}); err != nil {
		t.Fatal(err)
	}

	resolver := &modelresolve.Resolver{Cards: cards}
	cfg, err := resolver.Resolve(ctx, "default", "claude-sonnet-4-20250514")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "claude-sonnet-4-20250514" {
		t.Fatalf("model=%q", cfg.Model)
	}
	if cfg.Provider != "ant" || cfg.APIKey != "sk-default" {
		t.Fatalf("cfg=%+v", cfg)
	}
}

func TestResolveUnknownHandleWithoutDefault(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")

	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	cards := store.NewModelCardRepo(db)
	resolver := &modelresolve.Resolver{Cards: cards}
	cfg, err := resolver.Resolve(context.Background(), "default", "my-claude")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "my-claude" || cfg.APIKey != "" {
		t.Fatalf("cfg=%+v", cfg)
	}
}
