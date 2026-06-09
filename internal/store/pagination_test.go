package store_test

import (
	"testing"

	"github.com/open-ma/oma-building/internal/store"
)

func TestPageCursorRoundTrip(t *testing.T) {
	raw, err := store.EncodePageCursor(store.PageCursor{
		CreatedAt: 1710000000000,
		ID:        "agt_abc123",
	})
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := store.DecodePageCursor(raw)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.CreatedAt != 1710000000000 || decoded.ID != "agt_abc123" {
		t.Fatalf("decoded=%+v", decoded)
	}
}

func TestDecodePageCursorInvalid(t *testing.T) {
	if _, err := store.DecodePageCursor("not-a-cursor"); err == nil {
		t.Fatal("expected error")
	}
}

func TestClampLimit(t *testing.T) {
	if store.ClampLimit(0) != store.DefaultListLimit {
		t.Fatalf("zero limit")
	}
	if store.ClampLimit(999) != store.MaxListLimit {
		t.Fatalf("max limit")
	}
}
