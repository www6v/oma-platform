package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/store"
)

type meDeps struct {
	ConsoleDev bool
	ApiKeys    *store.ApiKeyRepo
}

func mountMeRoutes(r chi.Router, deps meDeps) {
	r.Get("/", func(w http.ResponseWriter, _ *http.Request) {
		if deps.ConsoleDev {
			writeJSON(w, http.StatusOK, map[string]any{
				"user": map[string]any{
					"id":    "default",
					"email": "default@local",
					"name":  "Default User",
					"role":  "owner",
				},
				"tenant": map[string]any{
					"id":   defaultTenant,
					"name": "Default",
				},
				"tenants": []map[string]any{
					{"id": defaultTenant, "name": "Default", "role": "owner"},
				},
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"user": map[string]any{
				"id":    "default",
				"email": "",
				"name":  "",
			},
			"tenant": map[string]any{
				"id":   defaultTenant,
				"name": "",
			},
			"tenants": []map[string]any{
				{"id": defaultTenant, "name": "", "role": "member"},
			},
		})
	})

	r.Get("/tenants", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{
			"data": []map[string]any{
				{"id": defaultTenant, "name": "Default", "role": "owner"},
			},
		})
	})

	r.Post("/cli-tokens", func(w http.ResponseWriter, req *http.Request) {
		if deps.ApiKeys == nil {
			writeError(w, http.StatusNotImplemented, "CLI tokens not implemented")
			return
		}
		var body struct {
			TenantID string `json:"tenant_id"`
			Name     string `json:"name"`
		}
		_ = json.NewDecoder(req.Body).Decode(&body)
		tenantID := body.TenantID
		if tenantID == "" {
			tenantID = defaultTenant
		}
		name := body.Name
		if name == "" {
			name = "CLI token"
		}
		minted, err := deps.ApiKeys.Mint(
			req.Context(), tenantID, "user_console_dev", name, "cli",
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":         minted.ID,
			"key":        minted.Key,
			"prefix":     minted.Prefix,
			"name":       minted.Name,
			"created_at": formatISO(minted.CreatedAt),
		})
	})
}

func mountApiKeyRoutes(r chi.Router, keys *store.ApiKeyRepo) {
	r.Post("/", func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(req.Body).Decode(&body)
		minted, err := keys.Mint(req.Context(), defaultTenant, "", body.Name, "")
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"id":         minted.ID,
			"name":       minted.Name,
			"key":        minted.Key,
			"prefix":     minted.Prefix,
			"created_at": formatISO(minted.CreatedAt),
		})
	})

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		list, err := keys.List(req.Context(), defaultTenant)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data := make([]map[string]any, 0, len(list))
		for _, k := range list {
			item := map[string]any{
				"id":         k.ID,
				"name":       k.Name,
				"prefix":     k.Prefix,
				"created_at": formatISO(k.CreatedAt),
			}
			if k.Source != "" {
				item["source"] = k.Source
			}
			data = append(data, item)
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": data})
	})

	r.Delete("/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		ok, err := keys.Delete(req.Context(), defaultTenant, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !ok {
			writeError(w, http.StatusNotFound, "API key not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"type": "api_key_deleted",
			"id":   id,
		})
	})
}

func formatISO(ms int64) string {
	return time.UnixMilli(ms).UTC().Format(time.RFC3339Nano)
}
