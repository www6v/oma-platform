package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMcpOAuthValidateSuccess(t *testing.T) {
	tokenSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "rotated-access",
			"refresh_token": "rotated-refresh",
			"expires_in":    7200,
		})
	}))
	defer tokenSrv.Close()

	handler := testRouter(t)

	vaultID := createVault(t, handler, "oauth-vault")
	credBody := `{
		"display_name": "OAuth MCP",
		"auth": {
			"type": "mcp_oauth",
			"mcp_server_url": "https://mcp.example.com",
			"access_token": "old-access",
			"refresh_token": "old-refresh",
			"token_endpoint": "` + tokenSrv.URL + `"
		}
	}`
	credID := createCredential(t, handler, vaultID, credBody)

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/vaults/"+vaultID+"/credentials/"+credID+"/mcp_oauth_validate",
		nil,
	)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("validate status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["type"] != "mcp_oauth_validation" || resp["validated"] != true {
		t.Fatalf("resp=%v", resp)
	}
	if int(resp["expires_in"].(float64)) != 7200 {
		t.Fatalf("expires_in=%v", resp["expires_in"])
	}
}

func TestMcpOAuthValidateBadCredentialType(t *testing.T) {
	handler := testRouter(t)
	vaultID := createVault(t, handler, "static-vault")
	credID := createCredential(t, handler, vaultID, `{
		"display_name": "Static",
		"auth": {"type": "static_bearer", "token": "secret"}
	}`)

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/vaults/"+vaultID+"/credentials/"+credID+"/mcp_oauth_validate",
		nil,
	)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMcpOAuthValidateTokenEndpointDown(t *testing.T) {
	handler := testRouter(t)
	vaultID := createVault(t, handler, "down-vault")
	credID := createCredential(t, handler, vaultID, `{
		"display_name": "OAuth down",
		"auth": {
			"type": "mcp_oauth",
			"mcp_server_url": "https://mcp.example.com",
			"refresh_token": "rt",
			"token_endpoint": "http://127.0.0.1:1/unreachable"
		}
	}`)

	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/vaults/"+vaultID+"/credentials/"+credID+"/mcp_oauth_validate",
		nil,
	)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func createVault(t *testing.T, handler http.Handler, name string) string {
	t.Helper()
	body := `{"name":"` + name + `"}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/vaults", bytes.NewBufferString(body),
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
	id, _ := vault["id"].(string)
	if id == "" {
		t.Fatal("missing vault id")
	}
	return id
}

func createCredential(
	t *testing.T,
	handler http.Handler,
	vaultID, body string,
) string {
	t.Helper()
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/vaults/"+vaultID+"/credentials",
		bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create credential status=%d body=%s", rec.Code, rec.Body.String())
	}
	var cred map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &cred); err != nil {
		t.Fatal(err)
	}
	id, _ := cred["id"].(string)
	if id == "" {
		t.Fatal("missing credential id")
	}
	return id
}
