package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness"
)

func TestModelCardCRUDAndKeyAPI(t *testing.T) {
	handler := testRouter(t)

	createBody := `{
		"model_id":"claude-prod",
		"model":"claude-sonnet-4-20250514",
		"provider":"ant",
		"api_key":"sk-test-9999",
		"is_default":true
	}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/model_cards", bytes.NewBufferString(createBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	cardID := created["id"].(string)
	if created["api_key_preview"] != "9999" {
		t.Fatalf("preview=%v", created["api_key_preview"])
	}
	if created["is_default"] != true {
		t.Fatalf("is_default=%v", created["is_default"])
	}

	dupReq := httptest.NewRequest(
		http.MethodPost, "/v1/model_cards", bytes.NewBufferString(createBody),
	)
	dupReq.Header.Set("Content-Type", "application/json")
	dupRec := httptest.NewRecorder()
	handler.ServeHTTP(dupRec, dupReq)
	if dupRec.Code != http.StatusConflict {
		t.Fatalf("duplicate status=%d body=%s", dupRec.Code, dupRec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/v1/model_cards", nil)
	listRec := httptest.NewRecorder()
	handler.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d", listRec.Code)
	}
	var listResp map[string]any
	_ = json.Unmarshal(listRec.Body.Bytes(), &listResp)
	if len(listResp["data"].([]any)) != 1 {
		t.Fatalf("list=%v", listResp["data"])
	}

	getReq := httptest.NewRequest(
		http.MethodGet, "/v1/model_cards/"+cardID, nil,
	)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRec.Code, getRec.Body.String())
	}

	keyReq := httptest.NewRequest(
		http.MethodGet, "/v1/model_cards/"+cardID+"/key", nil,
	)
	keyRec := httptest.NewRecorder()
	handler.ServeHTTP(keyRec, keyReq)
	if keyRec.Code != http.StatusOK {
		t.Fatalf("key status=%d body=%s", keyRec.Code, keyRec.Body.String())
	}
	var keyResp map[string]string
	_ = json.Unmarshal(keyRec.Body.Bytes(), &keyResp)
	if keyResp["api_key"] != "sk-test-9999" {
		t.Fatalf("api_key=%q", keyResp["api_key"])
	}

	patchBody := `{"model":"claude-opus-4-20250514"}`
	patchReq := httptest.NewRequest(
		http.MethodPost,
		"/v1/model_cards/"+cardID,
		bytes.NewBufferString(patchBody),
	)
	patchReq.Header.Set("Content-Type", "application/json")
	patchRec := httptest.NewRecorder()
	handler.ServeHTTP(patchRec, patchReq)
	if patchRec.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", patchRec.Code, patchRec.Body.String())
	}
	var patched map[string]any
	_ = json.Unmarshal(patchRec.Body.Bytes(), &patched)
	if patched["model"] != "claude-opus-4-20250514" {
		t.Fatalf("model=%v", patched["model"])
	}

	delReq := httptest.NewRequest(
		http.MethodDelete, "/v1/model_cards/"+cardID, nil,
	)
	delRec := httptest.NewRecorder()
	handler.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", delRec.Code, delRec.Body.String())
	}

	missingReq := httptest.NewRequest(
		http.MethodGet, "/v1/model_cards/"+cardID, nil,
	)
	missingRec := httptest.NewRecorder()
	handler.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusNotFound {
		t.Fatalf("deleted get status=%d", missingRec.Code)
	}
}

func TestSessionTurnResolvesModelCardHandle(t *testing.T) {
	recording := &harness.RecordingClient{FakeClient: harness.FakeClient{Text: "hi"}}
	handler, _ := testRouterHarness(t, recording)

	cardBody := `{
		"model_id":"claude-prod",
		"model":"claude-sonnet-4-20250514",
		"provider":"ant",
		"api_key":"sk-test-9999"
	}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/model_cards", bytes.NewBufferString(cardBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("card status=%d body=%s", rec.Code, rec.Body.String())
	}

	agentBody := `{
		"name":"card-agent",
		"model":"claude-prod",
		"system_prompt":"test"
	}`
	req = httptest.NewRequest(
		http.MethodPost, "/v1/agents", bytes.NewBufferString(agentBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("agent status=%d body=%s", rec.Code, rec.Body.String())
	}
	var agent map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &agent)

	sessBody := `{"agent":"` + agent["id"].(string) + `"}`
	req = httptest.NewRequest(
		http.MethodPost, "/v1/sessions", bytes.NewBufferString(sessBody),
	)
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

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if recording.RequestCount() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if recording.RequestCount() == 0 {
		t.Fatal("expected harness turn to run")
	}

	turnReq, _ := recording.LastRequest()
	if turnReq.Agent.Model != "claude-prod" {
		t.Fatalf("agent snapshot model=%q", turnReq.Agent.Model)
	}
	if turnReq.Model.Model != "claude-sonnet-4-20250514" {
		t.Fatalf("wire model=%q", turnReq.Model.Model)
	}
	if turnReq.Model.Provider != "ant" {
		t.Fatalf("provider=%q", turnReq.Model.Provider)
	}
	if turnReq.Model.APIKey != "sk-test-9999" {
		t.Fatalf("api_key=%q", turnReq.Model.APIKey)
	}
}
