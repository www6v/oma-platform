package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/mcpproxy"
	"github.com/open-ma/oma-building/internal/store"
)

type stubSessionStore struct {
	sess *store.Session
}

func (s stubSessionStore) Get(
	_ context.Context,
	_, _ string,
) (*store.Session, error) {
	if s.sess == nil {
		return nil, store.ErrNotFound
	}
	return s.sess, nil
}

func TestMcpProxyForwardsRequestBody(t *testing.T) {
	t.Parallel()

	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	snapshot, _ := json.Marshal(map[string]any{
		"mcp_servers": []map[string]string{
			{
				"name":                "smoke",
				"url":                 upstream.URL,
				"authorization_token": "tok",
			},
		},
	})

	r := chi.NewRouter()
	mountMcpProxyRoutes(r, mcpProxyDeps{
		Resolver: &mcpproxy.Resolver{
			Sessions: stubSessionStore{
				sess: &store.Session{AgentSnapshot: snapshot},
			},
		},
		APIKey: "dev-key",
	})

	payload := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/mcp-proxy/sess-1/smoke",
		bytes.NewReader(payload),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "dev-key")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if string(gotBody) != string(payload) {
		t.Fatalf("upstream body=%q want=%q", gotBody, payload)
	}
}

func TestMcpProxyVaultScopedCredential(t *testing.T) {

	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	agents := store.NewAgentRepo(db)
	envs := store.NewEnvironmentRepo(db)
	if err := envs.EnsureDefault(context.Background()); err != nil {
		t.Fatal(err)
	}
	sessions := store.NewSessionRepo(db, agents, envs)
	credentials := store.NewCredentialRepo(db)
	vaults := store.NewVaultRepo(db)

	ctx := context.Background()
	vault, err := vaults.Create(ctx, store.CreateVaultInput{
		TenantID: "default",
		Name:     "operate-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = credentials.Create(ctx, store.CreateCredentialInput{
		TenantID:    "default",
		VaultID:     vault.ID,
		DisplayName: "GitHub MCP",
		Auth: json.RawMessage(`{
			"type":"static_bearer",
			"mcp_server_url":"` + upstream.URL + `",
			"token":"vault-token-a"
		}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	agent, err := agents.Create(ctx, store.CreateAgentInput{
		TenantID: "default",
		Name:     "operate-agent",
		Model:    "claude-sonnet-4-20250514",
	})
	if err != nil {
		t.Fatal(err)
	}

	snapshot, _ := json.Marshal(map[string]any{
		"mcp_servers": []map[string]string{
			{"name": "github", "url": upstream.URL},
		},
	})
	sess, err := sessions.Create(ctx, store.CreateSessionInput{
		TenantID:      "default",
		AgentID:       agent.ID,
		EnvironmentID: store.DefaultEnvironmentID,
		VaultIDs:      []string{vault.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sessions.UpdateAgentSnapshot(
		ctx, "default", sess.ID, snapshot,
	); err != nil {
		t.Fatal(err)
	}
	sess.AgentSnapshot = snapshot

	r := chi.NewRouter()
	mountMcpProxyRoutes(r, mcpProxyDeps{
		Resolver: &mcpproxy.Resolver{
			Sessions:    sessions,
			Credentials: credentials,
		},
		APIKey: "dev-key",
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/mcp-proxy/"+sess.ID+"/github",
		bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "dev-key")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if gotAuth != "Bearer vault-token-a" {
		t.Fatalf("Authorization=%q want Bearer vault-token-a", gotAuth)
	}
}

func TestMcpProxyVaultIsolation(t *testing.T) {
	t.Parallel()

	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	agents := store.NewAgentRepo(db)
	envs := store.NewEnvironmentRepo(db)
	if err := envs.EnsureDefault(context.Background()); err != nil {
		t.Fatal(err)
	}
	sessions := store.NewSessionRepo(db, agents, envs)
	credentials := store.NewCredentialRepo(db)
	vaults := store.NewVaultRepo(db)

	ctx := context.Background()
	vaultA, err := vaults.Create(ctx, store.CreateVaultInput{
		TenantID: "default", Name: "vault-a",
	})
	if err != nil {
		t.Fatal(err)
	}
	vaultB, err := vaults.Create(ctx, store.CreateVaultInput{
		TenantID: "default", Name: "vault-b",
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = credentials.Create(ctx, store.CreateCredentialInput{
		TenantID: "default", VaultID: vaultA.ID, DisplayName: "A",
		Auth: json.RawMessage(`{
			"type":"static_bearer",
			"mcp_server_url":"` + upstream.URL + `",
			"token":"token-a"
		}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	_, err = credentials.Create(ctx, store.CreateCredentialInput{
		TenantID: "default", VaultID: vaultB.ID, DisplayName: "B",
		Auth: json.RawMessage(`{
			"type":"static_bearer",
			"mcp_server_url":"` + upstream.URL + `",
			"token":"token-b"
		}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	agent, err := agents.Create(ctx, store.CreateAgentInput{
		TenantID: "default",
		Name:     "operate-agent",
		Model:    "claude-sonnet-4-20250514",
	})
	if err != nil {
		t.Fatal(err)
	}

	snapshot, _ := json.Marshal(map[string]any{
		"mcp_servers": []map[string]string{
			{"name": "github", "url": upstream.URL},
		},
	})
	sess, err := sessions.Create(ctx, store.CreateSessionInput{
		TenantID:      "default",
		AgentID:       agent.ID,
		EnvironmentID: store.DefaultEnvironmentID,
		VaultIDs:      []string{vaultA.ID},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := sessions.UpdateAgentSnapshot(
		ctx, "default", sess.ID, snapshot,
	); err != nil {
		t.Fatal(err)
	}

	r := chi.NewRouter()
	mountMcpProxyRoutes(r, mcpProxyDeps{
		Resolver: &mcpproxy.Resolver{
			Sessions:    sessions,
			Credentials: credentials,
		},
		APIKey: "dev-key",
	})

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/mcp-proxy/"+sess.ID+"/github",
		bytes.NewReader([]byte(`{"jsonrpc":"2.0","id":1}`)),
	)
	req.Header.Set("x-api-key", "dev-key")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if gotAuth != "Bearer token-a" {
		t.Fatalf("Authorization=%q want Bearer token-a (not token-b)", gotAuth)
	}
}
