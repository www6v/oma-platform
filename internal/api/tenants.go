package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/store"
)

type tenantDeps struct {
	Tenants *store.TenantRepo
}

func mountTenantRoutes(r chi.Router, deps tenantDeps) {
	if deps.Tenants == nil {
		return
	}

	r.Post("/", func(w http.ResponseWriter, req *http.Request) {
		uid := userID(req)
		if uid == "" {
			writeError(
				w, http.StatusForbidden,
				"Cookie session required to create workspaces",
			)
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		created, err := deps.Tenants.CreateTenant(req.Context(), uid, body.Name)
		if err != nil {
			if err.Error() == "name is required" {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":         created.TenantID,
			"name":       created.Name,
			"role":       created.Role,
			"created_at": time.Now().UnixMilli(),
		})
	})
}
