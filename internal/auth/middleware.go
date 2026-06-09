package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/open-ma/oma-building/internal/store"
)

const fallbackTenant = "default"

var errNotMember = errors.New("not a member")

// TenantStore resolves tenant membership for cookie sessions.
type TenantStore interface {
	DefaultTenantForUser(ctx context.Context, userID string) (string, error)
	HasMembership(ctx context.Context, userID, tenantID string) (bool, error)
	EnsureTenant(
		ctx context.Context,
		userID string,
		name string,
		email string,
	) (string, error)
}

// Config controls API and cookie session authentication.
type Config struct {
	Disabled       bool
	APIKey         string
	ApiKeys        *store.ApiKeyRepo
	Tenants        TenantStore
	Session        *SessionResolver
	ConsoleMounted bool
}

// Middleware validates x-api-key or cookie sessions before /v1 routes.
func Middleware(cfg Config) func(http.Handler) http.Handler {
	if cfg.Disabled {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ctx := WithTenant(r.Context(), fallbackTenant)
				next.ServeHTTP(w, r.WithContext(ctx))
			})
		}
	}
	if cfg.APIKey == "" && cfg.ApiKeys == nil && cfg.Session == nil {
		return func(next http.Handler) http.Handler { return next }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isExempt(r.URL.Path, cfg.ConsoleMounted) {
				next.ServeHTTP(w, r)
				return
			}

			if key := strings.TrimSpace(r.Header.Get("x-api-key")); key != "" {
				tenantID, userID, ok := resolveAPIKey(r.Context(), cfg, key)
				if !ok {
					writeAuthError(w, http.StatusUnauthorized, "unauthorized")
					return
				}
				ctx := WithTenant(r.Context(), tenantID)
				if userID != "" {
					ctx = WithUser(ctx, User{ID: userID})
				}
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			if cfg.Session == nil {
				writeAuthError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			user, err := cfg.Session.Resolve(r.Context(), r.Header)
			if err != nil || user == nil {
				writeAuthError(w, http.StatusUnauthorized, "unauthorized")
				return
			}

			tenantID, err := resolveTenant(r.Context(), cfg, *user, r.Header)
			if err != nil {
				if errors.Is(err, errNotMember) {
					writeNotMember(w)
					return
				}
				writeAuthError(w, http.StatusInternalServerError, err.Error())
				return
			}

			ctx := WithUser(WithTenant(r.Context(), tenantID), *user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func resolveAPIKey(
	ctx context.Context,
	cfg Config,
	header string,
) (tenantID string, userID string, ok bool) {
	if cfg.APIKey != "" && header == cfg.APIKey {
		return fallbackTenant, "", true
	}
	if cfg.ApiKeys == nil {
		return "", "", false
	}
	sum := sha256.Sum256([]byte(header))
	hash := hex.EncodeToString(sum[:])
	rec, err := cfg.ApiKeys.FindByHash(ctx, hash)
	if err != nil || rec == nil {
		return "", "", false
	}
	tenant := rec.TenantID
	if tenant == "" {
		tenant = fallbackTenant
	}
	return tenant, rec.UserID, true
}

func resolveTenant(
	ctx context.Context,
	cfg Config,
	user User,
	headers http.Header,
) (string, error) {
	if cfg.Tenants == nil {
		return fallbackTenant, nil
	}
	requested := strings.TrimSpace(headers.Get("x-active-tenant"))
	if requested != "" {
		ok, err := cfg.Tenants.HasMembership(ctx, user.ID, requested)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", errNotMember
		}
		return requested, nil
	}
	tenantID, err := cfg.Tenants.DefaultTenantForUser(ctx, user.ID)
	if err != nil {
		return "", err
	}
	if tenantID != "" {
		return tenantID, nil
	}
	return cfg.Tenants.EnsureTenant(ctx, user.ID, user.Name, user.Email)
}

func isExempt(path string, consoleMounted bool) bool {
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

func writeAuthError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "error",
		"error": map[string]string{
			"type":    "authentication_error",
			"message": message,
		},
		"request_id": "req_auth",
	})
}

func writeNotMember(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"type": "error",
		"error": map[string]string{
			"type":    "not_a_member",
			"message": "Not a member of the requested tenant",
		},
	})
}
