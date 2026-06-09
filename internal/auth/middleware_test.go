package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/open-ma/oma-building/internal/auth"
)

func TestMiddlewareRequiresAPIKeyWhenEnabled(t *testing.T) {
	handler := auth.Middleware(auth.Config{
		APIKey: "secret",
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/agents", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/agents", nil)
	req.Header.Set("x-api-key", "secret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestMiddlewareDisabledSetsDefaultTenant(t *testing.T) {
	var got string
	handler := auth.Middleware(auth.Config{Disabled: true})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = auth.TenantFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest(http.MethodGet, "/v1/agents", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if got != "default" {
		t.Fatalf("tenant=%q", got)
	}
}
