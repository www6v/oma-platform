package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/integrations/linear"
	"github.com/open-ma/oma-building/internal/modelresolve"
	"github.com/open-ma/oma-building/internal/store"
)

type internalDeps struct {
	Secret         string
	Cards          *store.ModelCardRepo
	Resolver       *modelresolve.Resolver
	LinearGateway  *linear.Handler
}

func mountInternalRoutes(r chi.Router, deps internalDeps) {
	if deps.Cards == nil {
		return
	}
	r.Route("/v1/internal", func(r chi.Router) {
		r.Use(internalSecretMiddleware(deps.Secret))
		r.Route("/model_cards", func(r chi.Router) {
			r.Get("/resolve", handleInternalModelResolve(deps))
			r.Get("/{id}/key", handleInternalModelCardKey(deps))
		})
		if deps.LinearGateway != nil {
			r.Post(
				"/linear/publications/{pubId}/bind-mock-install",
				handleInternalLinearMockInstall(deps),
			)
		}
	})
}

func internalSecretMiddleware(secret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if secret == "" {
				writeError(w, http.StatusServiceUnavailable, "internal endpoints not configured")
				return
			}
			provided := r.Header.Get("x-internal-secret")
			if provided == "" || provided != secret {
				writeError(w, http.StatusUnauthorized, "unauthorized")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func internalTenantID(r *http.Request) string {
	if tid := r.URL.Query().Get("tenant_id"); tid != "" {
		return tid
	}
	return "default"
}

func handleInternalModelCardKey(deps internalDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		key, err := deps.Cards.GetAPIKey(r.Context(), internalTenantID(r), id)
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"api_key": key})
	}
}

func handleInternalModelResolve(deps internalDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		modelID := r.URL.Query().Get("model_id")
		if modelID == "" {
			writeError(w, http.StatusBadRequest, "model_id required")
			return
		}
		if deps.Resolver == nil {
			writeError(w, http.StatusServiceUnavailable, "model resolver not configured")
			return
		}
		cfg, err := deps.Resolver.Resolve(
			r.Context(),
			internalTenantID(r),
			modelID,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, modelConfigResponse(cfg))
	}
}

func handleInternalLinearMockInstall(deps internalDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pubID := chi.URLParam(r, "pubId")
		var body struct {
			WorkspaceID   string `json:"workspace_id"`
			WorkspaceName string `json:"workspace_name"`
			BotUserID     string `json:"bot_user_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if body.WorkspaceID == "" {
			body.WorkspaceID = "org_mock"
		}
		if body.WorkspaceName == "" {
			body.WorkspaceName = "Mock Workspace"
		}
		if body.BotUserID == "" {
			body.BotUserID = "bot_mock"
		}
		if err := deps.LinearGateway.BindMockInstallation(
			r.Context(), pubID,
			body.WorkspaceID, body.WorkspaceName, body.BotUserID,
		); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"publication_id": pubID,
			"status":         "live",
		})
	}
}

func modelConfigResponse(cfg harness.ModelConfig) map[string]any {
	out := map[string]any{
		"model":    cfg.Model,
		"provider": cfg.Provider,
	}
	if cfg.APIKey != "" {
		out["api_key"] = cfg.APIKey
	}
	if cfg.BaseURL != "" {
		out["base_url"] = cfg.BaseURL
	}
	if len(cfg.CustomHeaders) > 0 {
		var headers map[string]string
		if err := json.Unmarshal(cfg.CustomHeaders, &headers); err == nil {
			out["custom_headers"] = headers
		} else {
			out["custom_headers"] = json.RawMessage(cfg.CustomHeaders)
		}
	}
	return out
}
