package api

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/open-ma/oma-building/internal/store"
)

// AuthConfig controls API key enforcement and console-related exemptions.
type AuthConfig struct {
	APIKey         string
	ApiKeys        *store.ApiKeyRepo
	ConsoleMounted bool
	ConsoleDev     bool
}

// AuthMiddleware validates x-api-key when apiKey is non-empty.
func AuthMiddleware(cfg AuthConfig) func(http.Handler) http.Handler {
	if cfg.ConsoleDev || (cfg.APIKey == "" && cfg.ApiKeys == nil) {
		return func(next http.Handler) http.Handler { return next }
	}
	apiKey := cfg.APIKey
	keys := cfg.ApiKeys
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isAuthExempt(r.URL.Path, cfg.ConsoleMounted) {
				next.ServeHTTP(w, r)
				return
			}
			header := r.Header.Get("x-api-key")
			if header == "" {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			if apiKey != "" && header == apiKey {
				next.ServeHTTP(w, r)
				return
			}
			if keys != nil {
				sum := sha256.Sum256([]byte(header))
				hash := hex.EncodeToString(sum[:])
				rec, err := keys.FindByHash(r.Context(), hash)
				if err == nil && rec != nil {
					next.ServeHTTP(w, r)
					return
				}
			}
			writeError(w, http.StatusUnauthorized, "unauthorized")
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
