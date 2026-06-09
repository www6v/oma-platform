package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/auth"
	"github.com/open-ma/oma-building/internal/store"
)

type meDeps struct {
	AuthDisabled bool
	ApiKeys      *store.ApiKeyRepo
	Tenants      *store.TenantRepo
}

func mountMeRoutes(r chi.Router, deps meDeps) {
	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, http.StatusOK, buildMePayload(req.Context(), req, deps))
	})

	r.Get("/tenants", func(w http.ResponseWriter, req *http.Request) {
		data := listTenantPayload(req.Context(), req, deps)
		writeJSON(w, http.StatusOK, map[string]any{"data": data})
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
		tenant := body.TenantID
		if tenant == "" {
			tenant = tenantID(req)
		}
		name := body.Name
		if name == "" {
			name = "CLI token"
		}
		uid := userID(req)
		if uid == "" {
			uid = "user_cli"
		}
		minted, err := deps.ApiKeys.Mint(req.Context(), tenant, uid, name, "cli")
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

func buildMePayload(
	ctx context.Context,
	req *http.Request,
	deps meDeps,
) map[string]any {
	tenant := tenantID(req)
	tenantName := "Default"
	role := "owner"
	tenants := listTenantPayload(ctx, req, deps)

	if deps.AuthDisabled {
		return map[string]any{
			"user": map[string]any{
				"id":    "default",
				"email": "default@local",
				"name":  "Default User",
				"role":  "owner",
			},
			"tenant": map[string]any{
				"id":   tenant,
				"name": tenantName,
			},
			"tenants": tenants,
		}
	}

	user, ok := auth.UserFromContext(ctx)
	if !ok {
		return map[string]any{
			"user": map[string]any{
				"id":    "default",
				"email": "",
				"name":  "",
			},
			"tenant": map[string]any{
				"id":   tenant,
				"name": "",
			},
			"tenants": tenants,
		}
	}

	if deps.Tenants != nil {
		if name, err := deps.Tenants.GetTenantName(ctx, tenant); err == nil && name != "" {
			tenantName = name
		}
		for _, item := range tenants {
			if item["id"] == tenant {
				if r, ok := item["role"].(string); ok && r != "" {
					role = r
				}
				break
			}
		}
	}

	return map[string]any{
		"user": map[string]any{
			"id":    user.ID,
			"email": user.Email,
			"name":  user.Name,
			"role":  role,
		},
		"tenant": map[string]any{
			"id":   tenant,
			"name": tenantName,
		},
		"tenants": tenants,
	}
}

func listTenantPayload(
	ctx context.Context,
	req *http.Request,
	deps meDeps,
) []map[string]any {
	if deps.Tenants == nil {
		return []map[string]any{
			{"id": defaultTenant, "name": "Default", "role": "owner"},
		}
	}
	uid := userID(req)
	if uid == "" {
		return []map[string]any{
			{"id": tenantID(req), "name": "Default", "role": "owner"},
		}
	}
	items, err := deps.Tenants.ListForUser(ctx, uid)
	if err != nil || len(items) == 0 {
		return []map[string]any{
			{"id": tenantID(req), "name": "Default", "role": "owner"},
		}
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]any{
			"id":   item.TenantID,
			"name": item.Name,
			"role": item.Role,
		})
	}
	return out
}

func mountApiKeyRoutes(r chi.Router, keys *store.ApiKeyRepo) {
	r.Post("/", func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		_ = json.NewDecoder(req.Body).Decode(&body)
		minted, err := keys.Mint(req.Context(), tenantID(req), userID(req), body.Name, "")
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
		list, err := keys.List(req.Context(), tenantID(req))
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
		ok, err := keys.Delete(req.Context(), tenantID(req), id)
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
