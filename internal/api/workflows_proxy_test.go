package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
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
