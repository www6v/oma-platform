package store_test

import (
	"context"
	"testing"

	"github.com/open-ma/oma-building/internal/store"
)

func TestModelCardCreateAndResolveKey(t *testing.T) {
	db := openTestDB(t)
	repo := store.NewModelCardRepo(db.DB)
	ctx := context.Background()

	card, err := repo.Create(ctx, store.CreateModelCardInput{
		ModelID:  "claude-prod",
		Provider: "ant",
		APIKey:   "sk-test-1234",
	})
	if err != nil {
		t.Fatal(err)
	}
	if card.APIKeyPreview != "1234" {
		t.Fatalf("preview=%q", card.APIKeyPreview)
	}

	got, err := repo.GetByModelID(ctx, "default", "claude-prod")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.ID != card.ID {
		t.Fatalf("got=%+v", got)
	}

	key, err := repo.GetAPIKey(ctx, "default", card.ID)
	if err != nil {
		t.Fatal(err)
	}
	if key != "sk-test-1234" {
		t.Fatalf("key=%q", key)
	}
}

func TestModelCardDefaultUnique(t *testing.T) {
	db := openTestDB(t)
	repo := store.NewModelCardRepo(db.DB)
	ctx := context.Background()

	if _, err := repo.Create(ctx, store.CreateModelCardInput{
		ModelID:     "card-a",
		Provider:    "ant",
		APIKey:      "sk-a",
		MakeDefault: true,
	}); err != nil {
		t.Fatal(err)
	}
	second, err := repo.Create(ctx, store.CreateModelCardInput{
		ModelID:     "card-b",
		Provider:    "ant",
		APIKey:      "sk-b",
		MakeDefault: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !second.IsDefault {
		t.Fatal("expected second card to be default")
	}
	def, err := repo.GetDefault(ctx, "default")
	if err != nil {
		t.Fatal(err)
	}
	if def == nil || def.ID != second.ID {
		t.Fatalf("default=%+v", def)
	}
}
