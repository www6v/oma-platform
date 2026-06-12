package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/integrations/linear"
	"github.com/open-ma/oma-building/internal/store"
)

type linearGatewayDeps struct {
	Handler *linear.Handler
}

// NewLinearGatewayHandler wires OAuth + webhook dispatch when integrations
// and session handlers are available.
func NewLinearGatewayHandler(
	integrations *store.IntegrationRepo,
	sessions *sessionHandlers,
	origin, stateSecret string,
) *linear.Handler {
	if integrations == nil || sessions == nil || stateSecret == "" {
		return nil
	}
	if origin == "" {
		origin = integrationsGatewayOrigin()
	}
	return &linear.Handler{
		Integrations: integrations,
		Origin:       origin,
		StateSecret:  stateSecret,
		Dispatch:     &linearSessionDispatch{sessions: sessions},
	}
}

func mountLinearGatewayRoutes(r chi.Router, deps linearGatewayDeps) {
	if deps.Handler == nil {
		return
	}
	h := deps.Handler

	r.Get("/linear/oauth/pub/{pubId}/authorize", func(w http.ResponseWriter, req *http.Request) {
		pubID := chi.URLParam(req, "pubId")
		state := req.URL.Query().Get("state")
		if pubID == "" || state == "" {
			writeError(w, http.StatusBadRequest, "pubId and state required")
			return
		}
		target, err := h.AuthorizeRedirectURL(req.Context(), pubID, state)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		http.Redirect(w, req, target, http.StatusFound)
	})

	r.Get("/linear/oauth/pub/{pubId}/callback", func(w http.ResponseWriter, req *http.Request) {
		pubID := chi.URLParam(req, "pubId")
		q := req.URL.Query()
		if errParam := q.Get("error"); errParam != "" {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error":   "linear_oauth_denied",
				"details": errParam,
			})
			return
		}
		code := q.Get("code")
		state := q.Get("state")
		if pubID == "" || code == "" || state == "" {
			writeError(w, http.StatusBadRequest, "pubId, code, and state required")
			return
		}
		result, err := h.CompleteOAuth(req.Context(), pubID, code, state)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"error":   "install_failed",
				"details": err.Error(),
			})
			return
		}
		if result.ReturnURL == "" {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok":             true,
				"publication_id": result.PublicationID,
				"flow":           "complete",
			})
			return
		}
		target, err := url.Parse(result.ReturnURL)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid return_url")
			return
		}
		qv := target.Query()
		qv.Set("publication_id", result.PublicationID)
		qv.Set("install", "ok")
		target.RawQuery = qv.Encode()
		http.Redirect(w, req, target.String(), http.StatusFound)
	})

	r.Post("/linear/webhook/pub/{pubId}", func(w http.ResponseWriter, req *http.Request) {
		pubID := chi.URLParam(req, "pubId")
		rawBody, err := io.ReadAll(req.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}
		headers := map[string]string{}
		for key, values := range req.Header {
			if len(values) == 0 {
				continue
			}
			headers[strings.ToLower(key)] = values[0]
		}
		deliveryID := headers["linear-delivery"]
		if deliveryID == "" {
			deliveryID = parseWebhookID(rawBody)
		}
		outcome, err := h.HandleWebhook(
			req.Context(), pubID, deliveryID, headers, rawBody,
		)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"ok":     false,
				"reason": err.Error(),
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":             outcome.Handled,
			"reason":         outcome.Reason,
			"session_id":     nullIfEmpty(outcome.SessionID),
			"publication_id": nullIfEmpty(outcome.PublicationID),
		})
	})
}

func parseWebhookID(rawBody []byte) string {
	var envelope struct {
		WebhookID string `json:"webhookId"`
	}
	if err := json.Unmarshal(rawBody, &envelope); err != nil {
		return ""
	}
	return envelope.WebhookID
}
