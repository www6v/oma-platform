package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAgentCreateAMAShape(t *testing.T) {
	handler := testRouter(t)
	body := `{
		"name":"console-agent",
		"model":{"id":"claude-sonnet-4-20250514","speed":"fast"},
		"system":"You are helpful.",
		"description":"demo",
		"mcp_servers":[{"name":"demo","type":"url","url":"http://localhost"}],
		"skills":[{"type":"anthropic","skill_id":"code"}],
		"multiagent":{"type":"coordinator","agents":[{"type":"agent","id":"agent-other","version":2}]},
		"_oma":{"harness":"acp-proxy","runtime_binding":{"runtime_id":"rt1","acp_agent_id":"acp1"}}
	}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/agents", bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var agent map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &agent); err != nil {
		t.Fatal(err)
	}
	if agent["type"] != "agent" {
		t.Fatalf("type=%v", agent["type"])
	}
	model, ok := agent["model"].(map[string]any)
	if !ok {
		t.Fatalf("model=%v", agent["model"])
	}
	if model["id"] != "claude-sonnet-4-20250514" || model["speed"] != "fast" {
		t.Fatalf("model=%v", model)
	}
	if agent["system"] != "You are helpful." {
		t.Fatalf("system=%v", agent["system"])
	}
	if _, ok := agent["system_prompt"]; ok {
		t.Fatalf("unexpected system_prompt field")
	}
	if agent["created_at"] == nil {
		t.Fatalf("missing created_at")
	}
	if _, ok := agent["created_at"].(string); !ok {
		t.Fatalf("created_at not ISO string: %v", agent["created_at"])
	}
	if agent["archived_at"] != nil {
		t.Fatalf("archived_at=%v", agent["archived_at"])
	}
	skills, ok := agent["skills"].([]any)
	if !ok || len(skills) != 1 {
		t.Fatalf("skills=%v", agent["skills"])
	}
	mcp, ok := agent["mcp_servers"].([]any)
	if !ok || len(mcp) != 1 {
		t.Fatalf("mcp_servers=%v", agent["mcp_servers"])
	}
	multi, ok := agent["multiagent"].(map[string]any)
	if !ok || multi["type"] != "coordinator" {
		t.Fatalf("multiagent=%v", agent["multiagent"])
	}
	oma, ok := agent["_oma"].(map[string]any)
	if !ok || oma["harness"] != "acp-proxy" {
		t.Fatalf("_oma=%v", agent["_oma"])
	}
}

func TestAgentListHasMoreField(t *testing.T) {
	handler := testRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/agents?limit=50", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if _, ok := resp["has_more"]; !ok {
		t.Fatalf("missing has_more: %v", resp)
	}
	if resp["has_more"] != false {
		t.Fatalf("has_more=%v", resp["has_more"])
	}
}

func TestAgentVersionsAMAShape(t *testing.T) {
	handler := testRouter(t)

	createBody := `{"name":"v1","model":"claude-sonnet-4-20250514"}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/agents", bytes.NewBufferString(createBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var agent map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &agent)
	id := agent["id"].(string)

	patchBody := `{"name":"v2"}`
	req = httptest.NewRequest(
		http.MethodPost, "/v1/agents/"+id, bytes.NewBufferString(patchBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/agents/"+id+"/versions", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var listResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &listResp)
	data := listResp["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("versions=%v", data)
	}
	snap := data[0].(map[string]any)
	if snap["type"] != "agent" {
		t.Fatalf("snapshot type=%v", snap["type"])
	}
	if _, ok := snap["snapshot"]; ok {
		t.Fatalf("unexpected snapshot wrapper: %v", snap)
	}
	if snap["name"] != "v1" {
		t.Fatalf("snapshot name=%v", snap["name"])
	}

	req = httptest.NewRequest(
		http.MethodGet, "/v1/agents/"+id+"/versions/1", nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var version map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &version)
	if version["type"] != "agent" {
		t.Fatalf("version type=%v", version["type"])
	}
	if _, ok := version["agent_id"]; ok {
		t.Fatalf("unexpected version wrapper: %v", version)
	}
}
