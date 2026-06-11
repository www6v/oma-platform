package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuntimeConnectExchangeListDelete(t *testing.T) {
	handler := testRouter(t)
	state := "integration-state-12345"

	connectBody := `{"state":"` + state + `"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/runtimes/connect-runtime",
		bytes.NewBufferString(connectBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("connect status=%d body=%s", rec.Code, rec.Body.String())
	}
	var connectResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &connectResp); err != nil {
		t.Fatal(err)
	}
	code, _ := connectResp["code"].(string)
	if code == "" {
		t.Fatalf("missing code: %v", connectResp)
	}

	exchangeBody := map[string]any{
		"code":       code,
		"state":      state,
		"machine_id": "machine-test-001",
		"hostname":   "dev-mac",
		"os":         "darwin",
		"version":    "0.0.1-test",
	}
	rawExchange, _ := json.Marshal(exchangeBody)
	req = httptest.NewRequest(
		http.MethodPost,
		"/agents/runtime/exchange",
		bytes.NewReader(rawExchange),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("exchange status=%d body=%s", rec.Code, rec.Body.String())
	}
	var exchangeResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &exchangeResp); err != nil {
		t.Fatal(err)
	}
	runtimeID, _ := exchangeResp["runtime_id"].(string)
	token, _ := exchangeResp["token"].(string)
	agentKey, _ := exchangeResp["agent_api_key"].(string)
	if runtimeID == "" || token == "" || agentKey == "" {
		t.Fatalf("exchange resp=%v", exchangeResp)
	}
	if !strings.HasPrefix(token, "sk_machine_") {
		t.Fatalf("token prefix=%q", token)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/runtimes", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	runtimes, ok := listResp["runtimes"].([]any)
	if !ok || len(runtimes) != 1 {
		t.Fatalf("runtimes=%v", listResp["runtimes"])
	}
	first := runtimes[0].(map[string]any)
	if first["id"] != runtimeID {
		t.Fatalf("runtime id=%v", first["id"])
	}
	if first["hostname"] != "dev-mac" {
		t.Fatalf("hostname=%v", first["hostname"])
	}

	req = httptest.NewRequest(
		http.MethodGet,
		"/agents/runtime/me",
		nil,
	)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("me status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodDelete, "/v1/runtimes/"+runtimeID, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/runtimes", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var afterDelete map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &afterDelete)
	afterList, _ := afterDelete["runtimes"].([]any)
	if len(afterList) != 0 {
		t.Fatalf("expected empty runtimes after delete, got %v", afterList)
	}
}

func TestRuntimeExchangeRejectsBadCode(t *testing.T) {
	handler := testRouter(t)
	body := `{
		"code":"deadbeef",
		"state":"state-12345678",
		"machine_id":"m1",
		"hostname":"h",
		"os":"linux",
		"version":"1"
	}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/agents/runtime/exchange",
		bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestRuntimeConnectRequiresState(t *testing.T) {
	handler := testRouter(t)
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/runtimes/connect-runtime",
		bytes.NewBufferString(`{"state":"short"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
