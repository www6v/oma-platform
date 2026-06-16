package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/installbridge"
	"github.com/open-ma/oma-building/internal/integrations/oauthstate"
	"github.com/open-ma/oma-building/internal/store"
)

type installGatewayDeps struct {
	Bridge *installbridge.Bridge
}

func mountInstallGatewayRoutes(r chi.Router, deps installGatewayDeps) {
	if deps.Bridge == nil {
		return
	}
	b := deps.Bridge

	r.Get("/github/manifest/start/{formToken}", func(w http.ResponseWriter, req *http.Request) {
		token := chi.URLParam(req, "formToken")
		if token == "" {
			writeInstallHTML(w, http.StatusBadRequest,
				installbridge.InstallErrorPage("missing form token"))
			return
		}
		form, err := b.PrepareManifestForm(token)
		if err != nil {
			writeInstallHTML(w, http.StatusBadRequest,
				installbridge.InstallErrorPage("form token rejected: "+err.Error()))
			return
		}
		writeInstallHTML(w, http.StatusOK,
			installbridge.GitHubManifestStartPage(form))
	})

	r.Get("/github/manifest/callback", func(w http.ResponseWriter, req *http.Request) {
		code := req.URL.Query().Get("code")
		state := req.URL.Query().Get("state")
		if code == "" || state == "" {
			writeInstallHTML(w, http.StatusBadRequest,
				installbridge.InstallErrorPage("missing code or state"))
			return
		}
		result, err := b.CompleteManifestCallback(req.Context(), code, state)
		if err != nil {
			writeInstallHTML(w, http.StatusInternalServerError,
				installbridge.InstallErrorPage("manifest exchange failed: "+err.Error()))
			return
		}
		if result.ReturnURL == "" {
			writeInstallHTML(w, http.StatusOK, installbridge.InstallErrorPage(
				"install URL missing — restart the publish flow",
			))
			return
		}
		target, err := redirectInstallTarget(result.ReturnURL, result.PublicationID, nil)
		if err != nil || target == "" {
			writeInstallHTML(w, http.StatusInternalServerError,
				installbridge.InstallErrorPage("invalid redirect URL"))
			return
		}
		http.Redirect(w, req, target, http.StatusFound)
	})

	r.Get("/github/oauth/pub/{pubId}/callback", func(w http.ResponseWriter, req *http.Request) {
		pubID := chi.URLParam(req, "pubId")
		q := req.URL.Query()
		if errParam := q.Get("error"); errParam != "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "github_install_denied", "details": errParam,
			})
			return
		}
		installationID := q.Get("installation_id")
		setupAction := q.Get("setup_action")
		state := q.Get("state")
		if pubID == "" || installationID == "" || state == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "missing pubId, installation_id, or state",
			})
			return
		}
		if setupAction == "request" {
			writeInstallHTML(w, http.StatusOK,
				installbridge.GitHubRequestPendingPage(setupAction))
			return
		}
		result, err := b.CompleteGitHubOAuth(
			req.Context(), pubID, installationID, state,
		)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": "install_failed", "details": err.Error(),
			})
			return
		}
		handleInstallCompleteRedirect(w, req, result)
	})

	r.Get("/slack/oauth/pub/{pubId}/callback", func(w http.ResponseWriter, req *http.Request) {
		pubID := chi.URLParam(req, "pubId")
		q := req.URL.Query()
		if errParam := q.Get("error"); errParam != "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "slack_oauth_denied", "details": errParam,
			})
			return
		}
		code := q.Get("code")
		state := q.Get("state")
		if pubID == "" || code == "" || state == "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "missing pubId, code, or state",
			})
			return
		}
		result, err := b.CompleteSlackOAuth(req.Context(), pubID, code, state)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error": "install_failed", "details": err.Error(),
			})
			return
		}
		handleInstallCompleteRedirect(w, req, result)
	})

	r.Get("/github-setup/{token}", func(w http.ResponseWriter, req *http.Request) {
		token := chi.URLParam(req, "token")
		_, pub, err := verifyHandoffForm(req.Context(), b, token, "github.pub.form")
		if err != nil {
			writeInstallHTML(w, http.StatusBadRequest,
				installbridge.InstallErrorPage(err.Error()))
			return
		}
		writeInstallHTML(w, http.StatusOK,
			installbridge.GitHubSetupPage(token, pub.PersonaName))
	})

	r.Get("/slack-setup/{token}", func(w http.ResponseWriter, req *http.Request) {
		token := chi.URLParam(req, "token")
		_, pub, err := verifyHandoffForm(req.Context(), b, token, "slack.pub.form")
		if err != nil {
			writeInstallHTML(w, http.StatusBadRequest,
				installbridge.InstallErrorPage(err.Error()))
			return
		}
		writeInstallHTML(w, http.StatusOK,
			installbridge.SlackSetupPage(token, pub.PersonaName))
	})

	r.Post("/github/publications/credentials", func(w http.ResponseWriter, req *http.Request) {
		var in installbridge.GitHubCredentialsInput
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		out, err := b.SubmitGitHubCredentials(req.Context(), in)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "credentials_rejected", "details": err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, out)
	})

	r.Post("/slack/publications/credentials", func(w http.ResponseWriter, req *http.Request) {
		var in installbridge.SlackCredentialsInput
		if err := json.NewDecoder(req.Body).Decode(&in); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		out, err := b.SubmitSlackCredentials(req.Context(), in)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "credentials_rejected", "details": err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, out)
	})
}

func verifyHandoffForm(
	ctx context.Context,
	b *installbridge.Bridge,
	token, kind string,
) (oauthstate.FormTokenPayload, *store.IntegrationPublication, error) {
	form, err := oauthstate.VerifyFormToken(b.Secret, token, kind)
	if err != nil {
		return form, nil, err
	}
	provider := store.ProviderGitHub
	if kind == "slack.pub.form" {
		provider = store.ProviderSlack
	}
	pub, err := b.Integrations.GetPublication(ctx, provider, form.PublicationID)
	if err != nil {
		return form, nil, err
	}
	if pub == nil {
		return form, nil, installGatewayErr("publication not found")
	}
	return form, pub, nil
}

type installGatewayErr string

func (e installGatewayErr) Error() string { return string(e) }

func handleInstallCompleteRedirect(
	w http.ResponseWriter,
	req *http.Request,
	result installbridge.ContinueResult,
) {
	if result.ReturnURL == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "publicationId": result.PublicationID,
		})
		return
	}
	target, err := redirectInstallTarget(result.ReturnURL, result.PublicationID, map[string]string{
		"install": "ok",
	})
	if err != nil || target == "" {
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"error": "invalid_return_url",
		})
		return
	}
	http.Redirect(w, req, target, http.StatusFound)
}

func redirectInstallTarget(
	returnURL, publicationID string,
	extra map[string]string,
) (string, error) {
	u, err := url.Parse(returnURL)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("publication_id", publicationID)
	for k, v := range extra {
		if v != "" {
			q.Set(k, v)
		}
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func writeInstallHTML(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}
