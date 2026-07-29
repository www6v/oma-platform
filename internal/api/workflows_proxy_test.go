package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/open-ma/oma-building/internal/auth"
	"github.com/open-ma/oma-building/internal/harness"
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

func TestWorkflowStickyID(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/api/workflows", ""},
		{"/api/workflows/health", ""},
		{"/api/workflows/generate", ""},
		{"/api/workflows/validate", ""},
		{"/api/workflows/templates", ""},
		{"/api/workflows/templates/t1", ""},
		{"/api/workflows/wf-abc", "wf-abc"},
		{"/api/workflows/wf-abc/execute", "wf-abc"},
		{"/api/workflows/executions", ""},
		{"/api/workflows/executions/ex-1", "ex-1"},
		{"/api/workflows/executions/ex-1/ws", "ex-1"},
		{"/api/workflows/executions/ex-1/traces", "ex-1"},
		{"/api/workflows/executions/ex-1/cancel", "ex-1"},
	}
	for _, tc := range cases {
		if got := workflowStickyID(tc.path); got != tc.want {
			t.Fatalf("path=%q sticky=%q want %q", tc.path, got, tc.want)
		}
	}
}

func TestWorkflowsProxyInjectsStickyHeader(t *testing.T) {
	var gotSticky string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSticky = r.Header.Get(harness.StickySessionHeader)
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	proxy, err := NewWorkflowsProxy(upstream.URL)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/workflows/wf-42/execute", nil)
	rec := httptest.NewRecorder()
	proxy.ServeHTTP(rec, req)

	if gotSticky != "wf-42" {
		t.Fatalf("sticky=%q want wf-42", gotSticky)
	}
}
