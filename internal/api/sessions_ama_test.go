package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionAMAWireShape(t *testing.T) {
	handler := testRouter(t)

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/agents",
		bytes.NewBufferString(`{"name":"sess-wire","model":"claude-sonnet-4-6"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create agent: %d %s", rec.Code, rec.Body.String())
	}
	var agent map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &agent); err != nil {
		t.Fatal(err)
	}
	aid := agent["id"].(string)

	sessBody := `{"agent":{"id":"` + aid + `","version":1}}`
	req = httptest.NewRequest(
		http.MethodPost, "/v1/sessions", bytes.NewBufferString(sessBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create session: %d %s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created["type"] != "session" {
		t.Fatalf("type=%v", created["type"])
	}
	if _, ok := created["created_at"].(string); !ok {
		t.Fatalf("created_at not ISO string: %v", created["created_at"])
	}
	agentObj, ok := created["agent"].(map[string]any)
	if !ok {
		t.Fatalf("agent not object: %T", created["agent"])
	}
	if agentObj["id"] != aid {
		t.Fatalf("agent.id=%v", agentObj["id"])
	}
	if agentObj["name"] != "sess-wire" {
		t.Fatalf("agent.name=%v", agentObj["name"])
	}
	sid := created["id"].(string)

	req = httptest.NewRequest(http.MethodGet, "/v1/sessions/"+sid, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get session: %d", rec.Code)
	}
	var detail map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	detailAgent, ok := detail["agent"].(map[string]any)
	if !ok {
		t.Fatalf("detail agent not object: %T", detail["agent"])
	}
	if detailAgent["id"] != aid {
		t.Fatalf("detail agent.id=%v", detailAgent["id"])
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/sessions?limit=5", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list sessions: %d", rec.Code)
	}
	var list map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	data, ok := list["data"].([]any)
	if !ok || len(data) == 0 {
		t.Fatalf("list data: %v", list["data"])
	}
	row, ok := data[0].(map[string]any)
	if !ok {
		t.Fatal("row not object")
	}
	rowAgent, ok := row["agent"].(map[string]any)
	if !ok {
		t.Fatalf("list agent not object: %T", row["agent"])
	}
	if rowAgent["id"] == nil {
		t.Fatalf("list agent missing id")
	}

	req = httptest.NewRequest(http.MethodDelete, "/v1/sessions/"+sid, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	req = httptest.NewRequest(http.MethodDelete, "/v1/agents/"+aid, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
}
