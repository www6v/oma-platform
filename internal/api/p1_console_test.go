package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/open-ma/oma-building/internal/api"
	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/modelresolve"
	"github.com/open-ma/oma-building/internal/session"
	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/stream"
	"github.com/open-ma/oma-building/internal/workdir"
)

func TestStatsEndpoint(t *testing.T) {
	handler := testRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/stats", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]float64
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if _, ok := resp["agents"]; !ok {
		t.Fatalf("missing agents: %v", resp)
	}
}

func TestMeEndpoint(t *testing.T) {
	handler := testRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/me", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["tenant"] == nil {
		t.Fatalf("missing tenant: %v", resp)
	}
}

func TestSessionArchiveAndDelete(t *testing.T) {
	handler := testRouter(t)

	createAgent := `{"name":"a","model":"claude-sonnet-4-20250514"}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/agents", bytes.NewBufferString(createAgent),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var agent map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &agent)

	sessBody := `{"agent":"` + agent["id"].(string) + `"}`
	req = httptest.NewRequest(
		http.MethodPost, "/v1/sessions", bytes.NewBufferString(sessBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var sess map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &sess)
	sid := sess["id"].(string)

	req = httptest.NewRequest(
		http.MethodPost, "/v1/sessions/"+sid+"/archive", nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("archive status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/v1/sessions/"+sid, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAgentDeleteBlockedBySession(t *testing.T) {
	handler := testRouter(t)

	createAgent := `{"name":"a","model":"claude-sonnet-4-20250514"}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/agents", bytes.NewBufferString(createAgent),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var agent map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &agent)
	aid := agent["id"].(string)

	sessBody := `{"agent":"` + aid + `"}`
	req = httptest.NewRequest(
		http.MethodPost, "/v1/sessions", bytes.NewBufferString(sessBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	req = httptest.NewRequest(http.MethodDelete, "/v1/agents/"+aid, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestEventsListAMAShape(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	handler, _, sessions := testRouterSharedDB(t, db, nil)
	ctx := context.Background()
	agents := store.NewAgentRepo(db)
	environments := store.NewEnvironmentRepo(db)
	_ = environments.EnsureDefault(ctx)
	agent, err := agents.Create(ctx, store.CreateAgentInput{
		TenantID: "default",
		Name:     "a",
		Model:    "claude-sonnet-4-20250514",
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.Create(ctx, store.CreateSessionInput{
		TenantID: "default",
		AgentID:  agent.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	events := store.NewEventRepo(db)
	_, err = events.AppendEvents(ctx, sess.ID, []json.RawMessage{
		json.RawMessage(`{"type":"user.message","id":"e1","content":"hi"}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(
		http.MethodGet, "/v1/sessions/"+sess.ID+"/events?limit=10", nil,
	)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	data, ok := resp["data"].([]any)
	if !ok || len(data) != 1 {
		t.Fatalf("data=%v", resp["data"])
	}
	item := data[0].(map[string]any)
	if item["seq"] == nil || item["type"] == nil || item["ts"] == nil {
		t.Fatalf("item=%v", item)
	}
}

func testRouterWithApiKeys(t *testing.T) http.Handler {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	agents := store.NewAgentRepo(db)
	environments := store.NewEnvironmentRepo(db)
	if err := environments.EnsureDefault(context.Background()); err != nil {
		t.Fatal(err)
	}
	modelCards := store.NewModelCardRepo(db)
	apiKeys := store.NewApiKeyRepo(db)
	sessions := store.NewSessionRepo(db, agents, environments)
	events := store.NewEventRepo(db)
	hub := stream.NewHub()
	reg := session.NewRegistry()
	workdirs := workdir.NewManager(t.TempDir())
	models := &modelresolve.Resolver{Cards: modelCards}

	return api.NewRouter(api.Deps{
		Agents:       agents,
		Environments: environments,
		ModelCards:   modelCards,
		ApiKeys:      apiKeys,
		AuthDisabled: true,
		Sessions: api.NewSessionHandlers(
			sessions, events, hub, reg, workdirs, &harness.FakeClient{}, models,
		),
	})
}

func TestApiKeysCRUD(t *testing.T) {
	handler := testRouterWithApiKeys(t)

	req := httptest.NewRequest(
		http.MethodPost, "/v1/api_keys",
		bytes.NewBufferString(`{"name":"test"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id := created["id"].(string)

	req = httptest.NewRequest(http.MethodGet, "/v1/api_keys", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodDelete, "/v1/api_keys/"+id, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}
