package api

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/oauthflow"
	"github.com/open-ma/oma-building/internal/store"
)

func TestOAuthAuthorizeRequiresParams(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	vaults := store.NewVaultRepo(db)
	credentials := store.NewCredentialRepo(db)
	r := chi.NewRouter()
	mountOAuthV1Routes(r, oauthV1Deps{
		Vaults:      vaults,
		Credentials: credentials,
		State:       oauthflow.NewStateStore(),
		PublicURL:   "http://test",
	})

	req := httptest.NewRequest(http.MethodGet, "/authorize", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOAuthRefreshRequiresCredential(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	vaults := store.NewVaultRepo(db)
	credentials := store.NewCredentialRepo(db)
	r := chi.NewRouter()
	mountOAuthV1Routes(r, oauthV1Deps{
		Vaults:      vaults,
		Credentials: credentials,
		State:       oauthflow.NewStateStore(),
	})

	body := bytes.NewBufferString(`{"vault_id":"v1","credential_id":"c1"}`)
	req := httptest.NewRequest(http.MethodPost, "/refresh", body)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOAuthRoutesMountedOnRouter(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	vaults := store.NewVaultRepo(db)
	credentials := store.NewCredentialRepo(db)
	_, err = vaults.Create(context.Background(), store.CreateVaultInput{
		TenantID: "default",
		Name:     "test",
	})
	if err != nil {
		t.Fatal(err)
	}

	deps := Deps{
		Vaults:       vaults,
		Credentials:  credentials,
		OAuthState:   oauthflow.NewStateStore(),
		PublicURL:    "http://test",
		AuthDisabled: true,
	}
	handler := NewRouter(deps)

	req := httptest.NewRequest(
		http.MethodGet,
		"/v1/oauth/authorize?mcp_server_url=http://mcp&vault_id=missing",
		nil,
	)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
