package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVaultCRUDAndCredentials(t *testing.T) {
	handler := testRouter(t)

	createBody := `{"name":"prod-secrets"}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/vaults",
		bytes.NewBufferString(createBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create vault status=%d body=%s", rec.Code, rec.Body.String())
	}
	var vault map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &vault); err != nil {
		t.Fatal(err)
	}
	vaultID, _ := vault["id"].(string)
	if vaultID == "" {
		t.Fatal("missing vault id")
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/vaults", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list vaults status=%d", rec.Code)
	}
	var listResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	data, _ := listResp["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("expected 1 vault, got %d", len(data))
	}

	credBody := `{
		"display_name": "GitHub MCP",
		"auth": {
			"type": "mcp_oauth",
			"mcp_server_url": "https://mcp.example.com",
			"access_token": "secret-token"
		}
	}`
	req = httptest.NewRequest(
		http.MethodPost,
		"/v1/vaults/"+vaultID+"/credentials",
		bytes.NewBufferString(credBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create credential status=%d body=%s", rec.Code, rec.Body.String())
	}
	var cred map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &cred); err != nil {
		t.Fatal(err)
	}
	auth, _ := cred["auth"].(map[string]any)
	if auth["access_token"] != nil {
		t.Fatal("access_token should be stripped from API response")
	}
	credID, _ := cred["id"].(string)

	req = httptest.NewRequest(
		http.MethodGet,
		"/v1/vaults/"+vaultID+"/credentials",
		nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list credentials status=%d", rec.Code)
	}

	req = httptest.NewRequest(
		http.MethodPost,
		"/v1/vaults/"+vaultID+"/archive",
		nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("archive vault status=%d", rec.Code)
	}

	req = httptest.NewRequest(
		http.MethodDelete,
		"/v1/vaults/"+vaultID+"/credentials/"+credID,
		nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete credential status=%d", rec.Code)
	}
}
