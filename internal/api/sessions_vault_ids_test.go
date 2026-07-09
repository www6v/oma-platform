package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionVaultIDsRoundTrip(t *testing.T) {
	handler := testRouter(t)

	vaultBody := `{"name":"operate-session-vault"}`
	vaultReq := httptest.NewRequest(
		http.MethodPost, "/v1/vaults", bytes.NewBufferString(vaultBody),
	)
	vaultReq.Header.Set("Content-Type", "application/json")
	vaultRec := httptest.NewRecorder()
	handler.ServeHTTP(vaultRec, vaultReq)
	if vaultRec.Code != http.StatusCreated {
		t.Fatalf("vault status=%d body=%s", vaultRec.Code, vaultRec.Body.String())
	}
	var vault map[string]any
	if err := json.Unmarshal(vaultRec.Body.Bytes(), &vault); err != nil {
		t.Fatal(err)
	}
	vaultID, _ := vault["id"].(string)
	if vaultID == "" {
		t.Fatal("missing vault id")
	}

	agentBody := `{"name":"vault-session-agent","model":"claude-sonnet-4-20250514"}`
	agentReq := httptest.NewRequest(
		http.MethodPost, "/v1/agents", bytes.NewBufferString(agentBody),
	)
	agentReq.Header.Set("Content-Type", "application/json")
	agentRec := httptest.NewRecorder()
	handler.ServeHTTP(agentRec, agentReq)
	if agentRec.Code != http.StatusCreated {
		t.Fatalf("agent status=%d", agentRec.Code)
	}
	var agent map[string]any
	if err := json.Unmarshal(agentRec.Body.Bytes(), &agent); err != nil {
		t.Fatal(err)
	}
	agentID, _ := agent["id"].(string)

	sessBody, err := json.Marshal(map[string]any{
		"agent":     agentID,
		"vault_ids": []string{vaultID},
	})
	if err != nil {
		t.Fatal(err)
	}
	sessReq := httptest.NewRequest(
		http.MethodPost, "/v1/sessions", bytes.NewReader(sessBody),
	)
	sessReq.Header.Set("Content-Type", "application/json")
	sessRec := httptest.NewRecorder()
	handler.ServeHTTP(sessRec, sessReq)
	if sessRec.Code != http.StatusCreated {
		t.Fatalf("session status=%d body=%s", sessRec.Code, sessRec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(sessRec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	vaultIDs, ok := created["vault_ids"].([]any)
	if !ok || len(vaultIDs) != 1 || vaultIDs[0] != vaultID {
		t.Fatalf("create vault_ids=%v want [%s]", created["vault_ids"], vaultID)
	}
	sessionID, _ := created["id"].(string)

	getReq := httptest.NewRequest(
		http.MethodGet, "/v1/sessions/"+sessionID, nil,
	)
	getRec := httptest.NewRecorder()
	handler.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status=%d", getRec.Code)
	}
	var fetched map[string]any
	if err := json.Unmarshal(getRec.Body.Bytes(), &fetched); err != nil {
		t.Fatal(err)
	}
	got, ok := fetched["vault_ids"].([]any)
	if !ok || len(got) != 1 || got[0] != vaultID {
		t.Fatalf("get vault_ids=%v want [%s]", fetched["vault_ids"], vaultID)
	}
}
