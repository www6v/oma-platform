package api

import (
	"io"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/integrations/github"
	"github.com/open-ma/oma-building/internal/integrations/slack"
)

type githubGatewayDeps struct {
	Handler *github.Handler
}

type slackGatewayDeps struct {
	Handler *slack.Handler
}

func mountGitHubGatewayRoutes(r chi.Router, deps githubGatewayDeps) {
	if deps.Handler == nil {
		return
	}
	h := deps.Handler
	r.Post("/github/webhook/pub/{pubId}", func(w http.ResponseWriter, req *http.Request) {
		pubID := chi.URLParam(req, "pubId")
		rawBody, err := io.ReadAll(req.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}
		headers := webhookHeaders(req)
		deliveryID := headers["x-github-delivery"]
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

func mountSlackGatewayRoutes(r chi.Router, deps slackGatewayDeps) {
	if deps.Handler == nil {
		return
	}
	h := deps.Handler
	r.Post("/slack/webhook/pub/{pubId}", func(w http.ResponseWriter, req *http.Request) {
		pubID := chi.URLParam(req, "pubId")
		rawBody, err := io.ReadAll(req.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid body")
			return
		}
		headers := webhookHeaders(req)
		outcome, err := h.HandleWebhook(
			req.Context(), pubID, "", headers, rawBody,
		)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{
				"ok":     false,
				"reason": err.Error(),
			})
			return
		}
		if outcome.URLVerification {
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(outcome.Challenge))
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

func webhookHeaders(req *http.Request) map[string]string {
	headers := map[string]string{}
	for key, values := range req.Header {
		if len(values) == 0 {
			continue
		}
		headers[strings.ToLower(key)] = values[0]
	}
	return headers
}
