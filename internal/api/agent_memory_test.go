package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/api"
	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/store"
)

// Runs against MySQL when OMA_TEST_MYSQL_DSN is set (see store.OpenTestDB).

func testRouterAgentMemory(t *testing.T) (http.Handler, *store.MemoryStoreRepo) {
	t.Helper()
	tdb := store.OpenTestDB(t)
	deps, _ := testRouterDeps(t, tdb.DB, &harness.FakeClient{}, "", "")
	deps.InternalSecret = testInternalSecret
	return api.NewRouter(deps), deps.MemoryStores
}

func agentMemoryRequest(
	method, url, secret string,
	body any,
) *http.Request {
	var reader *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		reader = bytes.NewReader(raw)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, url, reader)
	req.Header.Set("Content-Type", "application/json")
	if secret != "" {
		req.Header.Set("x-internal-secret", secret)
	}
	return req
}

func TestInternalAgentMemoryWriteAndRead(t *testing.T) {
	handler, repo := testRouterAgentMemory(t)
	tenant := fmt.Sprintf("t-memtest-%d", time.Now().UnixNano())
	agentID := "memtest-agent-write-read"
	t.Cleanup(func() {
		_ = repo.DeleteStore(context.Background(), tenant, "agentmem-"+agentID)
	})

	writeBody := map[string]any{
		"tenant_id":  tenant,
		"agent_id":   agentID,
		"path":       "/MEMORY.md",
		"content":    "User prefers concise replies",
		"session_id": "sess-42",
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, agentMemoryRequest(
		http.MethodPost, "/v1/internal/agent_memory/write", testInternalSecret, writeBody,
	))
	if rec.Code != http.StatusOK {
		t.Fatalf("write status=%d body=%s", rec.Code, rec.Body.String())
	}
	var writeResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &writeResp); err != nil {
		t.Fatal(err)
	}
	memoryID, _ := writeResp["id"].(string)
	if memoryID == "" {
		t.Fatalf("write response missing id: %v", writeResp)
	}

	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, agentMemoryRequest(
		http.MethodGet,
		"/v1/internal/agent_memory?tenant_id="+tenant+"&agent_id="+agentID,
		testInternalSecret, nil,
	))
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got struct {
		StoreID  string            `json:"store_id"`
		Contents map[string]string `json:"contents"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.StoreID != "agentmem-"+agentID {
		t.Fatalf("unexpected store_id %q", got.StoreID)
	}
	if got.Contents["/MEMORY.md"] != "User prefers concise replies" {
		t.Fatalf("unexpected MEMORY.md content %q", got.Contents["/MEMORY.md"])
	}
	if got.Contents["/USER.md"] != "" {
		t.Fatalf("expected empty USER.md, got %q", got.Contents["/USER.md"])
	}

	// Write must be audited with the agent_session actor.
	versions, err := repo.ListVersions(context.Background(), tenant, got.StoreID, memoryID)
	if err != nil {
		t.Fatal(err)
	}
	if len(versions) == 0 {
		t.Fatal("expected version audit rows")
	}
	if versions[0].ActorType != "agent_session" || versions[0].ActorID != "sess-42" {
		t.Fatalf("unexpected actor: %s/%s", versions[0].ActorType, versions[0].ActorID)
	}
}

func TestInternalAgentMemoryValidation(t *testing.T) {
	handler, repo := testRouterAgentMemory(t)
	tenant := fmt.Sprintf("t-memtest-%d", time.Now().UnixNano())
	agentID := "memtest-agent-validation"
	t.Cleanup(func() {
		_ = repo.DeleteStore(context.Background(), tenant, "agentmem-"+agentID)
	})

	// Missing secret → 401.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, agentMemoryRequest(
		http.MethodGet,
		"/v1/internal/agent_memory?tenant_id="+tenant+"&agent_id="+agentID,
		"", nil,
	))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing secret status=%d", rec.Code)
	}

	// Missing agent_id → 400.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, agentMemoryRequest(
		http.MethodGet,
		"/v1/internal/agent_memory?tenant_id="+tenant,
		testInternalSecret, nil,
	))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing agent_id status=%d", rec.Code)
	}

	// Bad path → 400.
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, agentMemoryRequest(
		http.MethodPost, "/v1/internal/agent_memory/write", testInternalSecret,
		map[string]any{
			"tenant_id": tenant,
			"agent_id":  agentID,
			"path":      "/etc/passwd",
			"content":   "x",
		},
	))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad path status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAgentBuiltinStoresHiddenFromList(t *testing.T) {
	handler, repo := testRouterAgentMemory(t)
	// Public list routes resolve the tenant from auth context; in tests
	// that falls back to the default tenant, so use it end-to-end.
	tenant := "default"
	agentID := "memtest-agent-hidden"
	storeID := "agentmem-" + agentID
	t.Cleanup(func() {
		_ = repo.DeleteStore(context.Background(), tenant, storeID)
	})

	// Create the builtin store via the internal endpoint.
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, agentMemoryRequest(
		http.MethodGet,
		"/v1/internal/agent_memory?tenant_id="+tenant+"&agent_id="+agentID,
		testInternalSecret, nil,
	))
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Default listing hides builtin stores.
	req := httptest.NewRequest(http.MethodGet, "/v1/memory_stores?tenant_id="+tenant, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listed struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	for _, item := range listed.Data {
		if item["id"] == storeID {
			t.Fatalf("builtin store leaked into default list: %v", item)
		}
	}

	// include_builtin=true surfaces it with kind.
	req = httptest.NewRequest(
		http.MethodGet,
		"/v1/memory_stores?tenant_id="+tenant+"&include_builtin=true",
		nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list builtin status=%d", rec.Code)
	}
	listed = struct {
		Data []map[string]any `json:"data"`
	}{}
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range listed.Data {
		if item["id"] == storeID {
			found = true
			if item["kind"] != "agent_builtin" {
				t.Fatalf("unexpected kind: %v", item["kind"])
			}
		}
	}
	if !found {
		t.Fatalf("builtin store missing from include_builtin list: %v", listed.Data)
	}
}
