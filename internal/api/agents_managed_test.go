package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentCreate_Managed_KnownAgent(t *testing.T) {
	handler := testRouter(t)
	body := `{
		"name":"managed-hermes",
		"_oma":{"harness":"managed","runtime_binding":{"agent":"hermes"}}
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
}

func TestAgentCreate_Managed_UnknownAgent(t *testing.T) {
	handler := testRouter(t)
	body := `{
		"name":"managed-bogus",
		"_oma":{"harness":"managed","runtime_binding":{"agent":"bogus-agent"}}
	}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/agents", bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "must be one of") {
		t.Fatalf("expected 'must be one of' error, got body=%s", rec.Body.String())
	}
}

func TestAgentCreate_Managed_MissingRuntimeBinding(t *testing.T) {
	handler := testRouter(t)
	body := `{"name":"managed-nobinding","_oma":{"harness":"managed"}}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/agents", bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAgentUpdate_FlipToManaged_UnknownAgent(t *testing.T) {
	handler := testRouter(t)
	// Create a regular agent first.
	createBody := `{"name":"regular","model":{"id":"claude-sonnet-4-20250514","speed":"fast"}}`
	createReq := httptest.NewRequest(
		http.MethodPost, "/v1/agents", bytes.NewBufferString(createBody),
	)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup: create status=%d body=%s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["id"].(string)

	// Try to flip it to managed with an unknown agent.
	patchBody := `{"_oma":{"harness":"managed","runtime_binding":{"agent":"bogus-agent"}}}`
	patchReq := httptest.NewRequest(
		http.MethodPatch, "/v1/agents/"+id, bytes.NewBufferString(patchBody),
	)
	patchReq.Header.Set("Content-Type", "application/json")
	patchRec := httptest.NewRecorder()
	handler.ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got status=%d body=%s", patchRec.Code, patchRec.Body.String())
	}
}

func TestAgentUpdate_FlipToManaged_KnownAgent(t *testing.T) {
	handler := testRouter(t)
	createBody := `{"name":"regular","model":{"id":"claude-sonnet-4-20250514","speed":"fast"}}`
	createReq := httptest.NewRequest(
		http.MethodPost, "/v1/agents", bytes.NewBufferString(createBody),
	)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("setup: create status=%d body=%s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	id, _ := created["id"].(string)

	patchBody := `{"_oma":{"harness":"managed","runtime_binding":{"agent":"hermes"}}}`
	patchReq := httptest.NewRequest(
		http.MethodPatch, "/v1/agents/"+id, bytes.NewBufferString(patchBody),
	)
	patchReq.Header.Set("Content-Type", "application/json")
	patchRec := httptest.NewRecorder()
	handler.ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("expected 200, got status=%d body=%s", patchRec.Code, patchRec.Body.String())
	}
}

func TestAgentCreate_Managed_AllKnownAgents(t *testing.T) {
	for _, agent := range []string{"hermes", "openclaw", "claude-acp", "codex-acp"} {
		t.Run(agent, func(t *testing.T) {
			handler := testRouter(t)
			body := `{
				"name":"managed-` + agent + `",
				"_oma":{"harness":"managed","runtime_binding":{"agent":"` + agent + `"}}
			}`
			req := httptest.NewRequest(
				http.MethodPost, "/v1/agents", bytes.NewBufferString(body),
			)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusCreated {
				t.Fatalf("agent=%q status=%d body=%s", agent, rec.Code, rec.Body.String())
			}
		})
	}
}
