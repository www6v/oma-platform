package outbound_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/open-ma/oma-building/internal/outbound"
	"github.com/open-ma/oma-building/internal/store"
)

type stubSessions struct {
	sess *store.Session
}

func (s *stubSessions) Get(
	_ context.Context,
	_, _ string,
) (*store.Session, error) {
	return s.sess, nil
}

func TestResolverResolveByHostname(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	creds := store.NewCredentialRepo(db)
	_, err = creds.Create(ctx, store.CreateCredentialInput{
		TenantID:    "default",
		VaultID:     "vault-1",
		DisplayName: "mock-api",
		Auth: json.RawMessage(`{
			"type": "static_bearer",
			"mcp_server_url": "http://api.example.test:9888",
			"token": "secret-token"
		}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	resolver := &outbound.Resolver{
		Sessions: &stubSessions{
			sess: &store.Session{ID: "sess-1", TenantID: "default"},
		},
		Credentials: creds,
	}

	target, err := resolver.Resolve(
		ctx, "default", "sess-1", "api.example.test",
	)
	if err != nil {
		t.Fatal(err)
	}
	if target == nil || target.Token != "secret-token" {
		t.Fatalf("target=%v", target)
	}

	miss, err := resolver.Resolve(
		ctx, "default", "sess-1", "other.example.test",
	)
	if err != nil {
		t.Fatal(err)
	}
	if miss != nil {
		t.Fatalf("expected nil target, got %#v", miss)
	}
}
