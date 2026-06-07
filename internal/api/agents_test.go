package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/open-ma/oma-building/internal/api"
	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/session"
	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/stream"
	"github.com/open-ma/oma-building/internal/workdir"
)

func testRouter(t *testing.T) http.Handler {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	agents := store.NewAgentRepo(db)
	sessions := store.NewSessionRepo(db, agents)
	events := store.NewEventRepo(db)
	hub := stream.NewHub()
	reg := session.NewRegistry()
	workdirs := workdir.NewManager(t.TempDir())

	return api.NewRouter(api.Deps{
		Agents: agents,
		Sessions: api.NewSessionHandlers(
			sessions, events, hub, reg, workdirs, &harness.FakeClient{},
		),
	})
}

func TestPostAgent(t *testing.T) {
	handler := testRouter(t)
	body := `{"name":"demo","model":"claude-sonnet-4-20250514","system_prompt":"hi"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["version"].(float64) != 1 {
		t.Fatalf("version=%v", resp["version"])
	}
}

func TestPostSessionAndEvents(t *testing.T) {
	handler := testRouter(t)

	agentBody := `{"name":"s","model":"claude-sonnet-4-20250514"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewBufferString(agentBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("agent status=%d", rec.Code)
	}
	var agent map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &agent)

	sessBody := `{"agent":"` + agent["id"].(string) + `"}`
	req = httptest.NewRequest(http.MethodPost, "/v1/sessions", bytes.NewBufferString(sessBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("session status=%d body=%s", rec.Code, rec.Body.String())
	}
	var sess map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &sess)

	evBody := `{"events":[{"type":"user.message","content":[{"type":"text","text":"hi"}]}]}`
	path := "/v1/sessions/" + sess["id"].(string) + "/events"
	req = httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(evBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("events status=%d body=%s", rec.Code, rec.Body.String())
	}
}
