package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const dreamBetaHeader = "managed-agents-2026-04-01,dreaming-2026-04-21"

func TestDreamCreateListGetArchive(t *testing.T) {
	handler := testRouter(t)

	storeBody, _ := json.Marshal(map[string]any{
		"name": "dream-input",
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/memory_stores",
		bytes.NewReader(storeBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create store status=%d body=%s", rec.Code, rec.Body.String())
	}
	var storeResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &storeResp)
	storeID := storeResp["id"].(string)

	memBody, _ := json.Marshal(map[string]any{
		"path":    "/notes/a.md",
		"content": "dream source memory",
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
		t.Fatalf("write memory status=%d", rec.Code)
	}

	dreamBody, _ := json.Marshal(map[string]any{
		"inputs": []map[string]any{
			{"type": "memory_store", "memory_store_id": storeID},
		},
		"model": "claude-sonnet-4-6",
	})
	req = httptest.NewRequest(
		http.MethodPost,
		"/v1/dreams",
		bytes.NewReader(dreamBody),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("anthropic-beta", dreamBetaHeader)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create dream status=%d body=%s", rec.Code, rec.Body.String())
	}
	var dream map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &dream)
	dreamID := dream["id"].(string)
	if dream["type"] != "dream" {
		t.Fatalf("type=%v", dream["type"])
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		req = httptest.NewRequest(
			http.MethodGet,
			"/v1/dreams/"+dreamID,
			nil,
		)
		req.Header.Set("anthropic-beta", dreamBetaHeader)
		rec = httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("get dream status=%d", rec.Code)
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &dream)
		if dream["status"] == "completed" {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if dream["status"] != "completed" {
		t.Fatalf("dream status=%v", dream["status"])
	}
	outputs, ok := dream["outputs"].([]any)
	if !ok || len(outputs) == 0 {
		t.Fatalf("outputs=%v", dream["outputs"])
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/dreams", nil)
	req.Header.Set("anthropic-beta", dreamBetaHeader)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list dreams status=%d", rec.Code)
	}

	req = httptest.NewRequest(
		http.MethodPost,
		"/v1/dreams/"+dreamID+"/archive",
		nil,
	)
	req.Header.Set("anthropic-beta", dreamBetaHeader)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("archive dream status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestDreamRequiresBetaHeader(t *testing.T) {
	handler := testRouter(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/dreams", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", rec.Code)
	}
}
