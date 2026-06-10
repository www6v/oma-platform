package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/vaultoauth"
)

type vaultDeps struct {
	Vaults      *store.VaultRepo
	Credentials *store.CredentialRepo
	HTTPClient  *http.Client
}

func vaultHTTPClient(deps vaultDeps) *http.Client {
	if deps.HTTPClient != nil {
		return deps.HTTPClient
	}
	return http.DefaultClient
}

func mountVaultRoutes(r chi.Router, deps vaultDeps) {
	r.Post("/", func(w http.ResponseWriter, req *http.Request) {
		var body struct {
			Name string `json:"name"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if body.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		vault, err := deps.Vaults.Create(req.Context(), store.CreateVaultInput{
			TenantID: tenantID(req),
			Name:     body.Name,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, toAPIVault(vault))
	})

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		params := parseVaultListParams(req)
		if params.Err != "" {
			writeError(w, http.StatusBadRequest, params.Err)
			return
		}
		page, err := deps.Vaults.ListPage(req.Context(), params.Query)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		items := make([]map[string]any, 0, len(page.Items))
		for _, vault := range page.Items {
			items = append(items, toAPIVault(vault))
		}
		if page.NextCursor == "" {
			writeJSON(w, http.StatusOK, map[string]any{
				"data":     items,
				"has_more": false,
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"data":        items,
			"next_cursor": page.NextCursor,
			"has_more":    true,
		})
	})

	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, req *http.Request) {
			id := chi.URLParam(req, "id")
			vault, err := deps.Vaults.Get(req.Context(), tenantID(req), id)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if vault == nil {
				writeError(w, http.StatusNotFound, "Vault not found")
				return
			}
			writeJSON(w, http.StatusOK, toAPIVault(vault))
		})

		updateVault := func(w http.ResponseWriter, req *http.Request) {
			id := chi.URLParam(req, "id")
			var body struct {
				Name        *string `json:"name"`
				DisplayName *string `json:"display_name"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			name := ""
			if body.DisplayName != nil {
				name = *body.DisplayName
			} else if body.Name != nil {
				name = *body.Name
			}
			vault, err := deps.Vaults.Update(
				req.Context(), tenantID(req), id, name,
			)
			if err == store.ErrNotFound {
				writeError(w, http.StatusNotFound, "Vault not found")
				return
			}
			if err == store.ErrArchived {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, toAPIVault(vault))
		}
		r.Put("/", updateVault)
		r.Post("/", updateVault)

		r.Post("/archive", func(w http.ResponseWriter, req *http.Request) {
			id := chi.URLParam(req, "id")
			tenant := tenantID(req)
			vault, err := deps.Vaults.Archive(req.Context(), tenant, id)
			if err == store.ErrNotFound {
				writeError(w, http.StatusNotFound, "Vault not found")
				return
			}
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			if deps.Credentials != nil {
				_ = deps.Credentials.ArchiveByVault(req.Context(), tenant, id)
			}
			writeJSON(w, http.StatusOK, toAPIVault(vault))
		})

		r.Delete("/", func(w http.ResponseWriter, req *http.Request) {
			id := chi.URLParam(req, "id")
			err := deps.Vaults.Delete(req.Context(), tenantID(req), id)
			if err == store.ErrNotFound {
				writeError(w, http.StatusNotFound, "Vault not found")
				return
			}
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"type": "vault_deleted",
				"id":   id,
			})
		})

		mountCredentialRoutes(r, deps)
	})
}

func mountCredentialRoutes(r chi.Router, deps vaultDeps) {
	r.Post("/credentials", func(w http.ResponseWriter, req *http.Request) {
		vaultID := chi.URLParam(req, "id")
		tenant := tenantID(req)
		if !vaultExistsOr404(w, req, deps.Vaults, tenant, vaultID) {
			return
		}
		var body struct {
			DisplayName string          `json:"display_name"`
			Auth        json.RawMessage `json:"auth"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if body.DisplayName == "" || len(body.Auth) == 0 {
			writeError(
				w, http.StatusBadRequest,
				"display_name and auth are required",
			)
			return
		}
		cred, err := deps.Credentials.Create(req.Context(), store.CreateCredentialInput{
			TenantID:    tenant,
			VaultID:     vaultID,
			DisplayName: body.DisplayName,
			Auth:        body.Auth,
		})
		if err == store.ErrCredentialMaxExceeded {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err == store.ErrDuplicate {
			writeError(w, http.StatusConflict, err.Error())
			return
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, toAPICredential(cred))
	})

	r.Get("/credentials", func(w http.ResponseWriter, req *http.Request) {
		vaultID := chi.URLParam(req, "id")
		tenant := tenantID(req)
		if !vaultExistsOr404(w, req, deps.Vaults, tenant, vaultID) {
			return
		}
		list, err := deps.Credentials.List(req.Context(), tenant, vaultID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		items := make([]map[string]any, 0, len(list))
		for _, cred := range list {
			items = append(items, toAPICredential(cred))
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": items})
	})

	r.Route("/credentials/{credId}", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, req *http.Request) {
			vaultID := chi.URLParam(req, "id")
			credID := chi.URLParam(req, "credId")
			cred, err := deps.Credentials.Get(
				req.Context(), tenantID(req), vaultID, credID,
			)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if cred == nil {
				writeError(w, http.StatusNotFound, "Credential not found")
				return
			}
			writeJSON(w, http.StatusOK, toAPICredential(cred))
		})

		r.Post("/", func(w http.ResponseWriter, req *http.Request) {
			vaultID := chi.URLParam(req, "id")
			credID := chi.URLParam(req, "credId")
			var body struct {
				DisplayName *string          `json:"display_name"`
				Auth        *json.RawMessage `json:"auth"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			patch := store.UpdateCredentialInput{DisplayName: body.DisplayName}
			if body.Auth != nil {
				patch.Auth = *body.Auth
				patch.AuthSet = true
			}
			cred, err := deps.Credentials.Update(
				req.Context(), tenantID(req), vaultID, credID, patch,
			)
			if err == store.ErrNotFound {
				writeError(w, http.StatusNotFound, "Credential not found")
				return
			}
			if err == store.ErrImmutableField {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, toAPICredential(cred))
		})

		r.Post("/archive", func(w http.ResponseWriter, req *http.Request) {
			vaultID := chi.URLParam(req, "id")
			credID := chi.URLParam(req, "credId")
			cred, err := deps.Credentials.Archive(
				req.Context(), tenantID(req), vaultID, credID,
			)
			if err == store.ErrNotFound {
				writeError(w, http.StatusNotFound, "Credential not found")
				return
			}
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, toAPICredential(cred))
		})

		r.Post("/mcp_oauth_validate", func(w http.ResponseWriter, req *http.Request) {
			handleMcpOAuthValidate(w, req, deps)
		})

		r.Delete("/", func(w http.ResponseWriter, req *http.Request) {
			vaultID := chi.URLParam(req, "id")
			credID := chi.URLParam(req, "credId")
			err := deps.Credentials.Delete(
				req.Context(), tenantID(req), vaultID, credID,
			)
			if err == store.ErrNotFound {
				writeError(w, http.StatusNotFound, "Credential not found")
				return
			}
			if err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"type": "credential_deleted",
				"id":   credID,
			})
		})
	})
}

func handleMcpOAuthValidate(
	w http.ResponseWriter,
	req *http.Request,
	deps vaultDeps,
) {
	vaultID := chi.URLParam(req, "id")
	credID := chi.URLParam(req, "credId")
	tenant := tenantID(req)

	cred, err := deps.Credentials.Get(req.Context(), tenant, vaultID, credID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cred == nil {
		writeError(w, http.StatusNotFound, "Credential not found")
		return
	}

	meta, err := vaultoauth.RefreshMetadataOf(cred.Auth)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if meta == nil {
		writeError(
			w,
			http.StatusBadRequest,
			"Credential is not mcp_oauth or has no refresh_token / token_endpoint",
		)
		return
	}

	refreshed, err := vaultoauth.RefreshMcpOAuth(
		req.Context(),
		*meta,
		vaultHTTPClient(deps),
	)
	if err != nil {
		writeError(
			w,
			http.StatusBadGateway,
			"token_endpoint unreachable or refresh refused",
		)
		return
	}

	patch, err := vaultoauth.AuthPatchForRefresh(*refreshed)
	if err == nil {
		_, _ = deps.Credentials.Update(
			req.Context(), tenant, vaultID, credID,
			store.UpdateCredentialInput{Auth: patch, AuthSet: true},
		)
	}

	var expiresIn any
	if refreshed.ExpiresIn != nil {
		expiresIn = *refreshed.ExpiresIn
	} else {
		expiresIn = nil
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"type":       "mcp_oauth_validation",
		"validated":  true,
		"expires_in": expiresIn,
	})
}

func vaultExistsOr404(
	w http.ResponseWriter,
	req *http.Request,
	vaults *store.VaultRepo,
	tenant, vaultID string,
) bool {
	if vaults == nil {
		writeError(w, http.StatusNotFound, "Vault not found")
		return false
	}
	exists, err := vaults.Exists(req.Context(), tenant, vaultID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	if !exists {
		writeError(w, http.StatusNotFound, "Vault not found")
		return false
	}
	return true
}

func toAPIVault(vault *store.Vault) map[string]any {
	out := map[string]any{
		"type":       "vault",
		"id":         vault.ID,
		"name":       vault.Name,
		"created_at": formatISO(vault.CreatedAt),
	}
	if vault.UpdatedAt != nil {
		out["updated_at"] = formatISO(*vault.UpdatedAt)
	} else {
		out["updated_at"] = nil
	}
	if vault.ArchivedAt != nil {
		out["archived_at"] = formatISO(*vault.ArchivedAt)
	} else {
		out["archived_at"] = nil
	}
	return out
}

func toAPICredential(cred *store.Credential) map[string]any {
	out := map[string]any{
		"id":           cred.ID,
		"vault_id":     cred.VaultID,
		"display_name": cred.DisplayName,
		"auth":         json.RawMessage(store.StripCredentialSecrets(cred.Auth)),
		"created_at":   formatISO(cred.CreatedAt),
	}
	if cred.UpdatedAt != nil {
		out["updated_at"] = formatISO(*cred.UpdatedAt)
	} else {
		out["updated_at"] = nil
	}
	if cred.ArchivedAt != nil {
		out["archived_at"] = formatISO(*cred.ArchivedAt)
	} else {
		out["archived_at"] = nil
	}
	return out
}

type vaultListParams struct {
	Query store.VaultListQuery
	Err   string
}

func parseVaultListParams(r *http.Request) vaultListParams {
	q := r.URL.Query()
	status, includeArchived, statusErr := parseArchiveStatus(q)
	if statusErr != "" {
		return vaultListParams{Err: statusErr}
	}
	createdAfter, err := parseOptionalISO_ms(q.Get("created_after"))
	if err != nil {
		return vaultListParams{
			Err: "Invalid created_after '" + q.Get("created_after") +
				"'; expected ISO-8601 timestamp.",
		}
	}
	createdBefore, err := parseOptionalISO_ms(q.Get("created_before"))
	if err != nil {
		return vaultListParams{
			Err: "Invalid created_before '" + q.Get("created_before") +
				"'; expected ISO-8601 timestamp.",
		}
	}
	cursor := parseListCursor(q)
	if cursor != "" {
		if _, err := store.DecodePageCursor(cursor); err != nil {
			return vaultListParams{Err: err.Error()}
		}
	}
	return vaultListParams{
		Query: store.VaultListQuery{
			TenantID:        tenantID(r),
			Limit:           parseListLimit(q),
			Cursor:          cursor,
			Status:          status,
			Query:           q.Get("q"),
			CreatedAfter:    createdAfter,
			CreatedBefore:   createdBefore,
			IncludeArchived: includeArchived,
		},
	}
}
