package mcpproxy_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/open-ma/oma-building/internal/mcpproxy"
	"github.com/open-ma/oma-building/internal/store"
)

type fakeSessionRepo struct {
	sess *store.Session
}

func (f *fakeSessionRepo) Get(
	_ context.Context,
	_, _ string,
) (*store.Session, error) {
	return f.sess, nil
}

func TestResolveAuthorizationTokenInline(t *testing.T) {
	snapshot, _ := json.Marshal(map[string]any{
		"mcp_servers": []map[string]any{
			{
				"name":                "demo",
				"url":                 "https://mcp.example.com/mcp",
				"authorization_token": "inline-secret",
			},
		},
	})
	resolver := &mcpproxy.Resolver{
		Sessions: &fakeSessionRepo{
			sess: &store.Session{AgentSnapshot: snapshot},
		},
	}
	target, err := resolver.Resolve(
		context.Background(), "default", "sess-1", "demo",
	)
	if err != nil {
		t.Fatal(err)
	}
	if target.UpstreamURL != "https://mcp.example.com/mcp" {
		t.Fatalf("upstream=%q", target.UpstreamURL)
	}
	if target.UpstreamToken != "inline-secret" {
		t.Fatalf("token=%q", target.UpstreamToken)
	}
}

func TestResolveUnknownServer(t *testing.T) {
	snapshot, _ := json.Marshal(map[string]any{
		"mcp_servers": []map[string]any{},
	})
	resolver := &mcpproxy.Resolver{
		Sessions: &fakeSessionRepo{
			sess: &store.Session{AgentSnapshot: snapshot},
		},
	}
	_, err := resolver.Resolve(
		context.Background(), "default", "sess-1", "missing",
	)
	if err == nil {
		t.Fatal("expected error for unknown server")
	}
}
