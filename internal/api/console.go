package api

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
)

func mountConsoleDevRoutes(r chi.Router) {
	r.Get("/auth-info", handleAuthInfo)
	r.Get("/auth/get-session", handleConsoleDevSession)
	r.Post("/auth/sign-out", handleConsoleDevSignOut)
}

func handleAuthInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"providers":          []string{},
		"turnstile_site_key": nil,
	})
}

func handleConsoleDevSession(w http.ResponseWriter, _ *http.Request) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	writeJSON(w, http.StatusOK, map[string]any{
		"session": map[string]any{
			"id":        "sess_console_dev",
			"userId":    "user_console_dev",
			"token":     "console-dev",
			"expiresAt": "2099-01-01T00:00:00.000Z",
			"createdAt": now,
			"updatedAt": now,
		},
		"user": map[string]any{
			"id":            "user_console_dev",
			"name":          "Console Dev",
			"email":         "dev@localhost",
			"emailVerified": true,
			"createdAt":     now,
			"updatedAt":     now,
		},
	})
}

func handleConsoleDevSignOut(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}
