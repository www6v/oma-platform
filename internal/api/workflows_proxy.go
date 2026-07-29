package api

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/auth"
	"github.com/open-ma/oma-building/internal/harness"
)

// workflowStickyReserved are first path segments under /api/workflows that are
// not workflow ids (list/generate/health/templates/...).
var workflowStickyReserved = map[string]struct{}{
	"health":     {},
	"generate":   {},
	"templates":  {},
	"validate":   {},
	"executions": {},
}

// NewWorkflowsProxy forwards workflow REST and WebSocket traffic to the harness.
func NewWorkflowsProxy(harnessURL string) (http.Handler, error) {
	target, err := url.Parse(strings.TrimRight(harnessURL, "/"))
	if err != nil {
		return nil, err
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		http.Error(
			w,
			"workflow service unavailable: "+err.Error(),
			http.StatusBadGateway,
		)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if tenant := resolveWorkflowTenant(r); tenant != "" {
			r.Header.Set("X-Active-Tenant", tenant)
		}
		if sticky := workflowStickyID(r.URL.Path); sticky != "" {
			r.Header.Set(harness.StickySessionHeader, sticky)
		}
		r.URL.Host = target.Host
		r.URL.Scheme = target.Scheme
		r.Host = target.Host
		proxy.ServeHTTP(w, r)
	}), nil
}

// workflowStickyID extracts a stable affinity key from /api/workflows paths.
// Prefer execution id when present so traces/ws/cancel stay on one replica;
// otherwise use the workflow id for /{id} and /{id}/execute.
func workflowStickyID(path string) string {
	path = strings.Trim(path, "/")
	const prefix = "api/workflows/"
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	rest := strings.TrimPrefix(path, prefix)
	if rest == "" {
		return ""
	}
	parts := strings.Split(rest, "/")
	if len(parts) == 0 || parts[0] == "" {
		return ""
	}
	if parts[0] == "executions" {
		if len(parts) >= 2 && parts[1] != "" {
			return parts[1]
		}
		return ""
	}
	if _, reserved := workflowStickyReserved[parts[0]]; reserved {
		return ""
	}
	return parts[0]
}

func resolveWorkflowTenant(r *http.Request) string {
	if tid := auth.TenantFromContext(r.Context()); tid != "" {
		return tid
	}
	if tid := strings.TrimSpace(r.Header.Get("x-active-tenant")); tid != "" {
		return tid
	}
	if tid := strings.TrimSpace(r.Header.Get("x-oma-tenant-id")); tid != "" {
		return tid
	}
	return defaultTenant
}

func mountWorkflowsProxyRoutes(r chi.Router, harnessURL string) {
	if strings.TrimSpace(harnessURL) == "" {
		return
	}
	proxy, err := NewWorkflowsProxy(harnessURL)
	if err != nil {
		log.Printf("warning: workflows proxy: %v", err)
		return
	}
	r.Handle("/api/workflows", proxy)
	r.Handle("/api/workflows/*", proxy)
}
