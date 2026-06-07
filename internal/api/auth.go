package api

import (
	"net/http"
	"strings"
)

// AuthConfig controls API key enforcement and console-related exemptions.
type AuthConfig struct {
	APIKey         string
	ConsoleMounted bool
	ConsoleDev     bool
}

// AuthMiddleware validates x-api-key when apiKey is non-empty.
func AuthMiddleware(cfg AuthConfig) func(http.Handler) http.Handler {
	if cfg.ConsoleDev || cfg.APIKey == "" {
		return func(next http.Handler) http.Handler { return next }
	}
	apiKey := cfg.APIKey
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isAuthExempt(r.URL.Path, cfg.ConsoleMounted) {
				next.ServeHTTP(w, r)
				return
			}
			if r.Header.Get("x-api-key") != apiKey {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func isAuthExempt(path string, consoleMounted bool) bool {
	if path == "/health" || path == "/auth-info" {
		return true
	}
	if strings.HasPrefix(path, "/auth/") {
		return true
	}
	if consoleMounted && !strings.HasPrefix(path, "/v1/") {
		return true
	}
	return false
}
