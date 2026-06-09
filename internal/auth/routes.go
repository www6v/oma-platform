package auth

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
)

// RouteDeps configures auth HTTP routes on the platform router.
type RouteDeps struct {
	Disabled         bool
	AuthUpstream     string
	EmailOTP         bool
	GoogleConfigured bool
}

// Mount registers /auth-info and /auth/* handlers.
func Mount(r chi.Router, deps RouteDeps) error {
	r.Get("/auth-info", func(w http.ResponseWriter, _ *http.Request) {
		providers := []string{}
		if !deps.Disabled {
			providers = append(providers, "email")
			if deps.EmailOTP {
				providers = append(providers, "email-otp")
			}
			if deps.GoogleConfigured {
				providers = append(providers, "google")
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"providers":          providers,
			"turnstile_site_key": nil,
		})
	})

	if deps.Disabled {
		mountDisabledAuthStubs(r)
		return nil
	}
	if strings.TrimSpace(deps.AuthUpstream) == "" {
		return nil
	}
	proxy, err := NewAuthProxy(deps.AuthUpstream)
	if err != nil {
		return err
	}
	r.Handle("/auth/*", proxy)
	return nil
}

func mountDisabledAuthStubs(r chi.Router) {
	r.Get("/auth/get-session", handleDisabledSession)
	r.Post("/auth/sign-out", handleDisabledSignOut)
}

func handleDisabledSession(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"session": map[string]any{
			"id":        "sess_auth_disabled",
			"userId":    "user_auth_disabled",
			"token":     "auth-disabled",
			"expiresAt": "2099-01-01T00:00:00.000Z",
			"createdAt": "2020-01-01T00:00:00.000Z",
			"updatedAt": "2020-01-01T00:00:00.000Z",
		},
		"user": map[string]any{
			"id":            "user_auth_disabled",
			"name":          "Auth Disabled",
			"email":         "dev@localhost",
			"emailVerified": true,
			"createdAt":     "2020-01-01T00:00:00.000Z",
			"updatedAt":     "2020-01-01T00:00:00.000Z",
		},
	})
}

func handleDisabledSignOut(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// RouteDepsFromEnv builds route deps from process environment.
func RouteDepsFromEnv(disabled bool) RouteDeps {
	return RouteDeps{
		Disabled:         disabled,
		AuthUpstream:     os.Getenv("AUTH_UPSTREAM_URL"),
		EmailOTP:         os.Getenv("AUTH_REQUIRE_EMAIL_VERIFY") == "1",
		GoogleConfigured: os.Getenv("GOOGLE_CLIENT_ID") != "" &&
			os.Getenv("GOOGLE_CLIENT_SECRET") != "",
	}
}
