package api

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
)

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
		r.URL.Host = target.Host
		r.URL.Scheme = target.Scheme
		r.Host = target.Host
		proxy.ServeHTTP(w, r)
	}), nil
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
