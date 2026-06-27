package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/open-ma/oma-building/internal/auth"
)

func TestWorkflowsProxyForwardsHealth(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/workflows/health" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"status":"ok"}`)
	}))
	defer upstream.Close()

	proxy, err := NewWorkflowsProxy(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/workflows/health", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != `{"status":"ok"}` {
		t.Fatalf("body=%q", rec.Body.String())
	}
}

func TestWorkflowsProxyInjectsTenantHeader(t *testing.T) {
	var gotTenant string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTenant = r.Header.Get("X-Active-Tenant")
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	proxy, err := NewWorkflowsProxy(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/workflows/generate", nil)
	req = req.WithContext(auth.WithTenant(req.Context(), "tenant-from-auth"))
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if gotTenant != "tenant-from-auth" {
		t.Fatalf("tenant header=%q", gotTenant)
	}
}

func TestResolveWorkflowTenantDefaults(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/workflows", nil)
	if got := resolveWorkflowTenant(req); got != defaultTenant {
		t.Fatalf("tenant=%q want %q", got, defaultTenant)
	}
}
