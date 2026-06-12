package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/open-ma/oma-building/internal/api"
	"github.com/open-ma/oma-building/internal/modelresolve"
	"github.com/open-ma/oma-building/internal/store"
)

const testInternalSecret = "test-internal-secret"

func testRouterInternal(t *testing.T) http.Handler {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	deps, _ := testRouterDeps(t, db, nil, "")
	deps.InternalSecret = testInternalSecret
	deps.ModelResolver = &modelresolve.Resolver{Cards: deps.ModelCards}
	return api.NewRouter(deps)
}

func TestInternalRoutes503WhenPlatformSecretUnset(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	deps, _ := testRouterDeps(t, db, nil, "")
	deps.InternalSecret = ""
	deps.LinearGateway = nil
	handler := api.NewRouter(deps)

	req := httptest.NewRequest(
		http.MethodGet,
		"/v1/internal/model_cards/resolve?tenant_id=default&model_id=x",
		nil,
	)
	req.Header.Set("x-internal-secret", "any")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestInternalModelCardKeyRequiresSecret(t *testing.T) {
	handler := testRouterInternal(t)

	createBody := `{
		"model_id":"internal-key-test",
		"model":"claude-sonnet-4-20250514",
		"provider":"ant",
		"api_key":"sk-internal-1234"
	}`
	createReq := httptest.NewRequest(
		http.MethodPost,
		"/v1/model_cards",
		bytes.NewBufferString(createBody),
	)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRec.Code, createRec.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(createRec.Body.Bytes(), &created)
	cardID := created["id"].(string)

	noSecretReq := httptest.NewRequest(
		http.MethodGet,
		"/v1/internal/model_cards/"+cardID+"/key?tenant_id=default",
		nil,
	)
	noSecretRec := httptest.NewRecorder()
	handler.ServeHTTP(noSecretRec, noSecretReq)
	if noSecretRec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", noSecretRec.Code, noSecretRec.Body.String())
	}

	badSecretReq := httptest.NewRequest(
		http.MethodGet,
		"/v1/internal/model_cards/"+cardID+"/key?tenant_id=default",
		nil,
	)
	badSecretReq.Header.Set("x-internal-secret", "wrong")
	badSecretRec := httptest.NewRecorder()
	handler.ServeHTTP(badSecretRec, badSecretReq)
	if badSecretRec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", badSecretRec.Code)
	}

	okReq := httptest.NewRequest(
		http.MethodGet,
		"/v1/internal/model_cards/"+cardID+"/key?tenant_id=default",
		nil,
	)
	okReq.Header.Set("x-internal-secret", testInternalSecret)
	okRec := httptest.NewRecorder()
	handler.ServeHTTP(okRec, okReq)
	if okRec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", okRec.Code, okRec.Body.String())
	}
	var keyResp map[string]string
	_ = json.Unmarshal(okRec.Body.Bytes(), &keyResp)
	if keyResp["api_key"] != "sk-internal-1234" {
		t.Fatalf("api_key=%q", keyResp["api_key"])
	}
}

func TestInternalModelResolveByHandle(t *testing.T) {
	handler := testRouterInternal(t)

	createBody := `{
		"model_id":"claude-prod",
		"model":"claude-sonnet-4-20250514",
		"provider":"ant",
		"api_key":"sk-resolve-9999",
		"base_url":"https://api.example.test",
		"custom_headers":{"X-Custom":"abc"}
	}`
	createReq := httptest.NewRequest(
		http.MethodPost,
		"/v1/model_cards",
		bytes.NewBufferString(createBody),
	)
	createReq.Header.Set("Content-Type", "application/json")
	createRec := httptest.NewRecorder()
	handler.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", createRec.Code, createRec.Body.String())
	}

	resolveReq := httptest.NewRequest(
		http.MethodGet,
		"/v1/internal/model_cards/resolve?tenant_id=default&model_id=claude-prod",
		nil,
	)
	resolveReq.Header.Set("x-internal-secret", testInternalSecret)
	resolveRec := httptest.NewRecorder()
	handler.ServeHTTP(resolveRec, resolveReq)
	if resolveRec.Code != http.StatusOK {
		t.Fatalf("resolve status=%d body=%s", resolveRec.Code, resolveRec.Body.String())
	}
	var resolved map[string]any
	_ = json.Unmarshal(resolveRec.Body.Bytes(), &resolved)
	if resolved["model"] != "claude-sonnet-4-20250514" {
		t.Fatalf("model=%v", resolved["model"])
	}
	if resolved["provider"] != "ant" {
		t.Fatalf("provider=%v", resolved["provider"])
	}
	if resolved["api_key"] != "sk-resolve-9999" {
		t.Fatalf("api_key=%v", resolved["api_key"])
	}
	if resolved["base_url"] != "https://api.example.test" {
		t.Fatalf("base_url=%v", resolved["base_url"])
	}
	headers := resolved["custom_headers"].(map[string]any)
	if headers["X-Custom"] != "abc" {
		t.Fatalf("custom_headers=%v", resolved["custom_headers"])
	}
}
