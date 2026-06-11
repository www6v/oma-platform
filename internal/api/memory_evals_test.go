package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMemoryStoreCRUDFlow(t *testing.T) {
	handler := testRouter(t)

	createBody, _ := json.Marshal(map[string]any{
		"name":        "research-notes",
		"description": "Integration test store",
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/memory_stores",
		bytes.NewReader(createBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create store status=%d body=%s", rec.Code, rec.Body.String())
	}
	var store map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &store); err != nil {
		t.Fatal(err)
	}
	storeID := store["id"].(string)
	if store["type"] != "memory_store" {
		t.Fatalf("unexpected type: %v", store["type"])
	}

	memBody, _ := json.Marshal(map[string]any{
		"path":    "/notes/topic.md",
		"content": "hello memory",
	})
	req = httptest.NewRequest(
		http.MethodPost,
		"/v1/memory_stores/"+storeID+"/memories",
		bytes.NewReader(memBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("write memory status=%d body=%s", rec.Code, rec.Body.String())
	}
	var memory map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &memory); err != nil {
		t.Fatal(err)
	}
	memID := memory["id"].(string)

	req = httptest.NewRequest(
		http.MethodGet,
		"/v1/memory_stores/"+storeID+"/memories",
		nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list memories status=%d", rec.Code)
	}
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	items := list["data"].([]any)
	if len(items) != 1 {
		t.Fatalf("expected 1 memory, got %d", len(items))
	}
	meta := items[0].(map[string]any)
	if _, ok := meta["content"]; ok {
		t.Fatal("list response should omit content")
	}

	req = httptest.NewRequest(
		http.MethodGet,
		"/v1/memory_stores/"+storeID+"/memories/"+memID,
		nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get memory status=%d", rec.Code)
	}

	req = httptest.NewRequest(
		http.MethodGet,
		"/v1/memory_stores/"+storeID+"/memory_versions?memory_id="+memID,
		nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list versions status=%d", rec.Code)
	}

	req = httptest.NewRequest(
		http.MethodPost,
		"/v1/memory_stores/"+storeID+"/archive",
		nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("archive status=%d", rec.Code)
	}
}

func TestEvalRunCreateListDelete(t *testing.T) {
	handler := testRouter(t)

	createAgent := `{"name":"eval-agent","model":"claude-sonnet-4-20250514"}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/agents",
		bytes.NewBufferString(createAgent),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("agent create status=%d", rec.Code)
	}
	var agent map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &agent)
	agentID := agent["id"].(string)

	req = httptest.NewRequest(http.MethodGet, "/v1/environments?limit=5", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var envs map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &envs)
	envID := envs["data"].([]any)[0].(map[string]any)["id"].(string)

	runBody, _ := json.Marshal(map[string]any{
		"agent_id":       agentID,
		"environment_id": envID,
		"tasks": []map[string]any{
			{
				"id":       "smoke-task",
				"messages": []string{"Say hello"},
			},
		},
	})
	req = httptest.NewRequest(
		http.MethodPost,
		"/v1/evals/runs",
		bytes.NewReader(runBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create eval status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	runID := created["run_id"].(string)

	req = httptest.NewRequest(http.MethodGet, "/v1/evals/runs", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list evals status=%d", rec.Code)
	}
	var listed map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &listed)
	if len(listed["data"].([]any)) != 1 {
		t.Fatalf("expected one eval run")
	}

	req = httptest.NewRequest(
		http.MethodGet,
		"/v1/evals/runs/"+runID,
		nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get eval status=%d", rec.Code)
	}
	var detail map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &detail)
	if detail["status"] != "pending" {
		t.Fatalf("expected pending status, got %v", detail["status"])
	}

	req = httptest.NewRequest(
		http.MethodDelete,
		"/v1/evals/runs/"+runID,
		nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete eval status=%d", rec.Code)
	}
}
