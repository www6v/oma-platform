package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"

	"github.com/open-ma/oma-building/internal/store"
)

type internalCreateVaultCredentialBody struct {
	Action       string `json:"action"`
	UserID       string `json:"userId"`
	VaultName    string `json:"vaultName"`
	DisplayName  string `json:"displayName"`
	McpServerURL string `json:"mcpServerUrl"`
	BearerToken  string `json:"bearerToken"`
	Provider     string `json:"provider"`
}

type internalAddCapCliBody struct {
	Action      string            `json:"action"`
	UserID      string            `json:"userId"`
	VaultID     *string           `json:"vaultId"`
	VaultName   string            `json:"vaultName"`
	DisplayName string            `json:"displayName"`
	CliID       string            `json:"cliId"`
	Token       string            `json:"token"`
	ExpiresAt   *int64            `json:"expiresAt"`
	RefreshToken string           `json:"refreshToken"`
	Extras      map[string]string `json:"extras"`
	Provider    string            `json:"provider"`
}

type internalRotateVaultBody struct {
	Action   string `json:"action"`
	UserID   string `json:"userId"`
	VaultID  string `json:"vaultId"`
	NewToken string `json:"newToken"`
	CliID    string `json:"cliId"`
}

func handleInternalCreateVault(deps internalDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Vaults == nil || deps.Credentials == nil {
			writeError(w, http.StatusServiceUnavailable, "vaults not configured")
			return
		}

		raw, err := io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		var peek struct {
			Action string `json:"action"`
		}
		if err := json.Unmarshal(raw, &peek); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}

		switch peek.Action {
		case "create_with_credential":
			handleInternalCreateWithCredential(w, r.Context(), deps, raw)
		case "add_cap_cli":
			handleInternalAddCapCLI(w, r.Context(), deps, raw)
		default:
			writeError(w, http.StatusBadRequest, "unknown action")
		}
	}
}

func handleInternalCreateWithCredential(
	w http.ResponseWriter,
	ctx context.Context,
	deps internalDeps,
	raw []byte,
) {
	var body internalCreateVaultCredentialBody
	if err := json.Unmarshal(raw, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.UserID == "" || body.McpServerURL == "" || body.BearerToken == "" {
		writeError(w, http.StatusBadRequest, "userId, mcpServerUrl, bearerToken required")
		return
	}
	tenantID, err := resolveInternalTenantID(ctx, deps.Tenants, body.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tenantID == "" {
		writeError(w, http.StatusNotFound, "user has no tenant")
		return
	}

	vaultName := body.VaultName
	if vaultName == "" {
		vaultName = "integration-vault"
	}
	vault, err := deps.Vaults.Create(ctx, store.CreateVaultInput{
		TenantID: tenantID,
		Name:     vaultName,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	auth := map[string]any{
		"type":           "static_bearer",
		"mcp_server_url": body.McpServerURL,
		"token":          body.BearerToken,
	}
	if body.Provider != "" {
		auth["provider"] = body.Provider
	}
	authJSON, err := json.Marshal(auth)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	cred, err := deps.Credentials.Create(ctx, store.CreateCredentialInput{
		TenantID:    tenantID,
		VaultID:     vault.ID,
		DisplayName: body.DisplayName,
		Auth:        authJSON,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"vaultId":      vault.ID,
		"credentialId": cred.ID,
	})
}

func handleInternalAddCapCLI(
	w http.ResponseWriter,
	ctx context.Context,
	deps internalDeps,
	raw []byte,
) {
	var body internalAddCapCliBody
	if err := json.Unmarshal(raw, &body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.UserID == "" || body.CliID == "" || body.Token == "" {
		writeError(w, http.StatusBadRequest, "userId, cliId, token required")
		return
	}
	tenantID, err := resolveInternalTenantID(ctx, deps.Tenants, body.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if tenantID == "" {
		writeError(w, http.StatusNotFound, "user has no tenant")
		return
	}

	vaultID := ""
	if body.VaultID != nil {
		vaultID = *body.VaultID
	}
	if vaultID == "" {
		vaultName := body.VaultName
		if vaultName == "" {
			vaultName = "integration-vault"
		}
		vault, vaultErr := deps.Vaults.Create(ctx, store.CreateVaultInput{
			TenantID: tenantID,
			Name:     vaultName,
		})
		if vaultErr != nil {
			writeError(w, http.StatusInternalServerError, vaultErr.Error())
			return
		}
		vaultID = vault.ID
	} else {
		ok, existsErr := deps.Vaults.Exists(ctx, tenantID, vaultID)
		if existsErr != nil {
			writeError(w, http.StatusInternalServerError, existsErr.Error())
			return
		}
		if !ok {
			writeError(w, http.StatusNotFound, "vault not found in tenant")
			return
		}
	}

	auth := map[string]any{
		"type":    "cap_cli",
		"cli_id":  body.CliID,
		"token":   body.Token,
	}
	if body.ExpiresAt != nil {
		auth["expires_at"] = time.UnixMilli(*body.ExpiresAt).UTC().Format(time.RFC3339)
	}
	if body.RefreshToken != "" {
		auth["refresh_token"] = body.RefreshToken
	}
	if len(body.Extras) > 0 {
		auth["extras"] = body.Extras
	}
	if body.Provider != "" {
		auth["provider"] = body.Provider
	}
	authJSON, err := json.Marshal(auth)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	cred, err := deps.Credentials.Create(ctx, store.CreateCredentialInput{
		TenantID:    tenantID,
		VaultID:     vaultID,
		DisplayName: body.DisplayName,
		Auth:        authJSON,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"vaultId":      vaultID,
		"credentialId": cred.ID,
	})
}

func handleInternalRotateVault(deps internalDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Vaults == nil || deps.Credentials == nil {
			writeError(w, http.StatusServiceUnavailable, "vaults not configured")
			return
		}
		var body internalRotateVaultBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if body.UserID == "" || body.VaultID == "" || body.NewToken == "" {
			writeError(w, http.StatusBadRequest, "userId, vaultId, newToken required")
			return
		}
		if body.Action != "rotate_bearer" && body.Action != "rotate_cap_cli" {
			writeError(w, http.StatusBadRequest, "unknown action")
			return
		}

		tenantID, err := resolveInternalTenantID(r.Context(), deps.Tenants, body.UserID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if tenantID == "" {
			writeError(w, http.StatusNotFound, "user has no tenant")
			return
		}

		list, err := deps.Credentials.List(r.Context(), tenantID, body.VaultID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if len(list) == 0 {
			writeError(w, http.StatusNotFound, "vault has no credentials")
			return
		}

		target := findRotateTarget(list, body.Action, body.CliID)
		if target == nil {
			writeError(w, http.StatusNotFound, "matching credential not found")
			return
		}

		patch, err := json.Marshal(map[string]string{"token": body.NewToken})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		updated, err := deps.Credentials.Update(
			r.Context(),
			tenantID,
			body.VaultID,
			target.ID,
			store.UpdateCredentialInput{Auth: patch, AuthSet: true},
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":           true,
			"credentialId": updated.ID,
		})
	}
}

func findRotateTarget(
	list []*store.Credential,
	action string,
	cliID string,
) *store.Credential {
	for _, cred := range list {
		var auth map[string]any
		if err := json.Unmarshal(cred.Auth, &auth); err != nil {
			continue
		}
		authType, _ := auth["type"].(string)
		switch action {
		case "rotate_bearer":
			if authType == "static_bearer" {
				return cred
			}
		case "rotate_cap_cli":
			if authType != "cap_cli" {
				continue
			}
			id, _ := auth["cli_id"].(string)
			if cliID == "" || id == cliID {
				return cred
			}
		}
	}
	return nil
}
