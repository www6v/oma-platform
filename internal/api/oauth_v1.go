package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/oauthflow"
	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/vaultoauth"
)

type oauthV1Deps struct {
	Vaults      *store.VaultRepo
	Credentials *store.CredentialRepo
	State       *oauthflow.StateStore
	HTTPClient  *http.Client
	PublicURL   string
}

func mountOAuthV1Routes(r chi.Router, deps oauthV1Deps) {
	if deps.Vaults == nil || deps.Credentials == nil {
		return
	}
	client := deps.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	state := deps.State
	if state == nil {
		state = oauthflow.NewStateStore()
	}

	r.Get("/authorize", func(w http.ResponseWriter, req *http.Request) {
		handleOAuthAuthorize(w, req, deps, client, state)
	})
	r.Get("/callback", func(w http.ResponseWriter, req *http.Request) {
		handleOAuthCallback(w, req, deps, client, state)
	})
	r.Post("/refresh", func(w http.ResponseWriter, req *http.Request) {
		handleOAuthRefresh(w, req, deps, client)
	})
}

func handleOAuthAuthorize(
	w http.ResponseWriter,
	req *http.Request,
	deps oauthV1Deps,
	client *http.Client,
	state *oauthflow.StateStore,
) {
	mcpServerURL := req.URL.Query().Get("mcp_server_url")
	vaultID := req.URL.Query().Get("vault_id")
	credentialID := req.URL.Query().Get("credential_id")
	clientRedirect := req.URL.Query().Get("redirect_uri")
	if mcpServerURL == "" || vaultID == "" {
		writeError(w, http.StatusBadRequest, "mcp_server_url and vault_id are required")
		return
	}

	tenant := tenantID(req)
	vault, err := deps.Vaults.Get(req.Context(), tenant, vaultID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if vault == nil {
		writeError(w, http.StatusNotFound, "Vault not found")
		return
	}

	baseURL := publicBaseURL(req, deps.PublicURL)
	callbackURI := baseURL + "/v1/oauth/callback"

	meta, err := oauthflow.DiscoverOAuthMeta(req.Context(), client, mcpServerURL)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": fmt.Sprintf("OAuth discovery failed: %s", err.Error()),
		})
		return
	}

	callerClientID := req.URL.Query().Get("client_id")
	callerClientSecret := req.URL.Query().Get("client_secret")
	clientID := callerClientID
	clientSecret := callerClientSecret

	if clientID == "" && meta.AuthServer.RegistrationEndpoint != "" {
		id, secret, ok := oauthflow.DynamicClientRegistration(
			req.Context(),
			client,
			meta.AuthServer.RegistrationEndpoint,
			callbackURI,
		)
		if ok {
			clientID = id
			clientSecret = secret
		}
	}

	if clientID == "" {
		preset, presetErr := oauthflow.ResolvePresetClient(
			meta.AuthServer.Issuer,
			callbackURI,
		)
		if presetErr != nil {
			writeJSON(w, http.StatusNotImplemented, map[string]string{
				"error": presetErr.Error(),
			})
			return
		}
		if preset != nil {
			clientID = preset.ClientID
			clientSecret = preset.ClientSecret
		}
	}

	if clientID == "" {
		writeJSON(w, http.StatusNotImplemented, map[string]string{
			"error": fmt.Sprintf(
				"MCP server %s does not support Dynamic Client Registration and no preset client_id is configured for issuer %s.",
				mcpServerURL, meta.AuthServer.Issuer,
			),
		})
		return
	}

	codeVerifier, err := oauthflow.RandomHex(64)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	codeChallenge := oauthflow.Sha256Base64URL(codeVerifier)
	stateToken, err := oauthflow.RandomHex(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	redirectAfter := clientRedirect
	if redirectAfter == "" {
		redirectAfter = baseURL + "/"
	}

	state.Put(stateToken, oauthflow.PendingState{
		TenantID:            tenant,
		VaultID:             vaultID,
		CredentialID:        credentialID,
		McpServerURL:        mcpServerURL,
		CodeVerifier:        codeVerifier,
		ClientID:            clientID,
		ClientSecret:        clientSecret,
		TokenEndpoint:       meta.AuthServer.TokenEndpoint,
		AuthorizationServer: meta.AuthServer.Issuer,
		RedirectURI:         redirectAfter,
		ResourceURI:         meta.Resource.Resource,
	})

	authURL, err := url.Parse(meta.AuthServer.AuthorizationEndpoint)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	q := authURL.Query()
	q.Set("response_type", "code")
	q.Set("client_id", clientID)
	q.Set("redirect_uri", callbackURI)
	q.Set("state", stateToken)
	q.Set("code_challenge", codeChallenge)
	q.Set("code_challenge_method", "S256")
	q.Set("resource", meta.Resource.Resource)
	if len(meta.Resource.ScopesSupported) > 0 {
		q.Set("scope", strings.Join(meta.Resource.ScopesSupported, " "))
	}
	authURL.RawQuery = q.Encode()
	http.Redirect(w, req, authURL.String(), http.StatusFound)
}

func handleOAuthCallback(
	w http.ResponseWriter,
	req *http.Request,
	deps oauthV1Deps,
	client *http.Client,
	state *oauthflow.StateStore,
) {
	if errParam := req.URL.Query().Get("error"); errParam != "" {
		desc := req.URL.Query().Get("error_description")
		if desc == "" {
			desc = errParam
		}
		writeOAuthCloseHTML(
			w, http.StatusBadRequest, "Authorization failed", desc,
			"", "", false, "", "",
		)
		return
	}

	code := req.URL.Query().Get("code")
	stateToken := req.URL.Query().Get("state")
	if code == "" || stateToken == "" {
		writeError(w, http.StatusBadRequest, "code and state are required")
		return
	}

	oauthState, ok := state.Get(stateToken)
	if !ok {
		writeError(w, http.StatusBadRequest, "Invalid or expired OAuth state")
		return
	}

	baseURL := publicBaseURL(req, deps.PublicURL)
	callbackURI := baseURL + "/v1/oauth/callback"

	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", callbackURI)
	form.Set("client_id", oauthState.ClientID)
	form.Set("code_verifier", oauthState.CodeVerifier)
	form.Set("resource", oauthState.ResourceURI)
	if oauthState.ClientSecret != "" {
		form.Set("client_secret", oauthState.ClientSecret)
	}

	tokenReq, err := http.NewRequestWithContext(
		req.Context(),
		http.MethodPost,
		oauthState.TokenEndpoint,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		state.Delete(stateToken)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	tokenReq.Header.Set("Accept", "application/json")

	tokenRes, err := client.Do(tokenReq)
	if err != nil {
		state.Delete(stateToken)
		writeOAuthCloseHTML(
			w, http.StatusBadGateway, "Token exchange failed", err.Error(),
			"", "", false, "", "",
		)
		return
	}
	defer tokenRes.Body.Close()
	if tokenRes.StatusCode < 200 || tokenRes.StatusCode >= 300 {
		errBody, _ := io.ReadAll(io.LimitReader(tokenRes.Body, 4096))
		state.Delete(stateToken)
		writeOAuthCloseHTML(
			w, http.StatusBadGateway, "Token exchange failed", string(errBody),
			"", "", false, "", "",
		)
		return
	}

	var tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    *int   `json:"expires_in"`
	}
	if err := json.NewDecoder(tokenRes.Body).Decode(&tokens); err != nil {
		state.Delete(stateToken)
		writeOAuthCloseHTML(
			w, http.StatusBadGateway, "Token exchange failed", err.Error(),
			"", "", false, "", "",
		)
		return
	}

	var expiresAt string
	if tokens.ExpiresIn != nil {
		expiresAt = time.Now().Add(
			time.Duration(*tokens.ExpiresIn) * time.Second,
		).UTC().Format(time.RFC3339Nano)
	}

	mcpHost := ""
	if u, err := url.Parse(oauthState.McpServerURL); err == nil {
		mcpHost = u.Hostname()
	}
	serverName := strings.TrimSuffix(mcpHost, ".com")
	serverName = strings.TrimSuffix(serverName, ".app")
	serverName = strings.TrimPrefix(serverName, "mcp.")

	credAuth := map[string]any{
		"type":                  "mcp_oauth",
		"mcp_server_url":        oauthState.McpServerURL,
		"access_token":          tokens.AccessToken,
		"refresh_token":         tokens.RefreshToken,
		"token_endpoint":        oauthState.TokenEndpoint,
		"client_id":             oauthState.ClientID,
		"client_secret":         oauthState.ClientSecret,
		"expires_at":            expiresAt,
		"authorization_server":  oauthState.AuthorizationServer,
	}
	authJSON, err := json.Marshal(credAuth)
	if err != nil {
		state.Delete(stateToken)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if oauthState.CredentialID != "" {
		_, _ = deps.Credentials.Update(
			req.Context(),
			oauthState.TenantID,
			oauthState.VaultID,
			oauthState.CredentialID,
			store.UpdateCredentialInput{Auth: authJSON, AuthSet: true},
		)
	} else {
		_, err = deps.Credentials.Create(req.Context(), store.CreateCredentialInput{
			TenantID:    oauthState.TenantID,
			VaultID:     oauthState.VaultID,
			DisplayName: serverName + " (OAuth)",
			Auth:        authJSON,
		})
		if err != nil {
			state.Delete(stateToken)
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}

	state.Delete(stateToken)

	probeOK, probeMsg := oauthflow.ProbeMcpServer(
		req.Context(), client, oauthState.McpServerURL, tokens.AccessToken,
	)

	redirectURL, _ := url.Parse(oauthState.RedirectURI)
	if redirectURL != nil {
		q := redirectURL.Query()
		q.Set("oauth", "success")
		q.Set("service", serverName)
		q.Set("probe_ok", map[bool]string{true: "1", false: "0"}[probeOK])
		if probeMsg != "" {
			q.Set("probe_message", probeMsg)
		}
		redirectURL.RawQuery = q.Encode()
	}

	fallback := ""
	if redirectURL != nil {
		fallback = redirectURL.String()
	}
	writeOAuthCloseHTML(
		w, http.StatusOK, "Connected", serverName,
		oauthState.VaultID, serverName, probeOK, probeMsg,
		fallback,
	)
}

func handleOAuthRefresh(
	w http.ResponseWriter,
	req *http.Request,
	deps oauthV1Deps,
	client *http.Client,
) {
	var body struct {
		VaultID      string `json:"vault_id"`
		CredentialID string `json:"credential_id"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.VaultID == "" || body.CredentialID == "" {
		writeError(w, http.StatusBadRequest, "vault_id and credential_id are required")
		return
	}

	tenant := tenantID(req)
	cred, err := deps.Credentials.Get(
		req.Context(), tenant, body.VaultID, body.CredentialID,
	)
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
			w, http.StatusBadRequest,
			"Credential is not mcp_oauth type",
		)
		return
	}

	refreshed, err := vaultoauth.RefreshMcpOAuth(
		req.Context(),
		*meta,
		client,
	)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{
			"error": "Token refresh failed",
		})
		return
	}

	patch, err := vaultoauth.AuthPatchForRefresh(*refreshed)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_, err = deps.Credentials.Update(
		req.Context(), tenant, body.VaultID, body.CredentialID,
		store.UpdateCredentialInput{Auth: patch, AuthSet: true},
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	out := map[string]any{"access_token": refreshed.AccessToken}
	if refreshed.ExpiresIn != nil {
		expiresAt := time.Now().Add(
			time.Duration(*refreshed.ExpiresIn) * time.Second,
		).UTC().Format(time.RFC3339Nano)
		out["expires_at"] = expiresAt
	}
	writeJSON(w, http.StatusOK, out)
}

func publicBaseURL(req *http.Request, override string) string {
	if override != "" {
		return strings.TrimRight(override, "/")
	}
	if v := strings.TrimSpace(req.Header.Get("X-Forwarded-Proto")); v != "" {
		host := req.Host
		if h := req.Header.Get("X-Forwarded-Host"); h != "" {
			host = h
		}
		return v + "//" + host
	}
	scheme := "http"
	if req.TLS != nil {
		scheme = "https"
	}
	return scheme + "//" + req.Host
}

func writeOAuthCloseHTML(
	w http.ResponseWriter,
	status int,
	title string,
	body string,
	vaultID string,
	service string,
	probeOK bool,
	probeMsg string,
	fallbackURL string,
) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	msgJSON, _ := json.Marshal(probeMsg)
	fallbackJSON, _ := json.Marshal(fallbackURL)
	redirectJS := ""
	if fallbackURL != "" {
		redirectJS = fmt.Sprintf("window.location.href = %s;", string(fallbackJSON))
	}
	fmt.Fprintf(w, `<html><body>
<p>%s</p>
<script>
(function(){
  var msg = {
    type: "oauth_complete",
    service: %q,
    vault_id: %q,
    probe_ok: %t,
    probe_message: %s
  };
  var notified = false;
  try {
    if (window.opener && !window.opener.closed) {
      window.opener.postMessage(msg, "*");
      notified = true;
    }
  } catch (e) {}
  try {
    var bc = new BroadcastChannel("openma-oauth");
    bc.postMessage(msg);
    bc.close();
    notified = true;
  } catch (e) {}
  if (notified) {
    window.close();
  } else {
    %s
  }
})();
</script>
<p>%s</p>
</body></html>`, title, service, vaultID, probeOK, string(msgJSON),
		redirectJS, body)
}
