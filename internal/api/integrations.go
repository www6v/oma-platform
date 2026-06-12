package api

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/open-ma/oma-building/internal/integrations/linear"
	"github.com/open-ma/oma-building/internal/store"
)

type integrationsDeps struct {
	Integrations  *store.IntegrationRepo
	GatewayOrigin string
	Linear        *linear.Handler
}

func mountIntegrationRoutes(r chi.Router, deps integrationsDeps) {
	if deps.Integrations == nil {
		mountIntegrationStubRoutes(r)
		return
	}
	origin := deps.GatewayOrigin
	if origin == "" {
		origin = "http://127.0.0.1:8787"
	}

	r.Route("/v1/integrations", func(r chi.Router) {
		for _, provider := range []store.IntegrationProvider{
			store.ProviderLinear,
			store.ProviderGitHub,
			store.ProviderSlack,
		} {
			p := provider
			r.Route("/"+string(p), func(r chi.Router) {
				mountProviderIntegrationRoutes(
					r, p, deps.Integrations, origin, deps.Linear,
				)
			})
		}
	})
}

func mountProviderIntegrationRoutes(
	r chi.Router,
	provider store.IntegrationProvider,
	repo *store.IntegrationRepo,
	origin string,
	linearHandler *linear.Handler,
) {
	r.Use(requireIntegrationUser)

	r.Get("/installations", func(w http.ResponseWriter, req *http.Request) {
		list, err := repo.ListInstallationsByUser(
			req.Context(), userID(req), provider,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data := make([]map[string]any, 0, len(list))
		for _, item := range list {
			data = append(data, serializeInstallation(item))
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": data})
	})

	r.Get("/installations/{id}/publications", func(w http.ResponseWriter, req *http.Request) {
		uid := userID(req)
		installationID := chi.URLParam(req, "id")
		inst, err := repo.GetInstallation(req.Context(), provider, installationID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if inst == nil || inst.UserID != uid {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		list, err := repo.ListPublicationsByInstallation(
			req.Context(), provider, installationID,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"data": serializePublications(list),
		})
	})

	r.Get("/agents/{id}/publications", func(w http.ResponseWriter, req *http.Request) {
		list, err := repo.ListPublicationsByUserAndAgent(
			req.Context(), provider, userID(req), chi.URLParam(req, "id"),
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"data": serializePublications(list),
		})
	})

	r.Get("/publications", func(w http.ResponseWriter, req *http.Request) {
		status := req.URL.Query().Get("status")
		if status == "" {
			writeJSON(w, http.StatusOK, map[string]any{"data": []any{}})
			return
		}
		if status != "pending" {
			writeError(w, http.StatusBadRequest,
				"only ?status=pending is supported on this endpoint")
			return
		}
		list, err := repo.ListPendingPublicationsByUser(
			req.Context(), provider, userID(req),
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"data": serializePublications(list),
		})
	})

	r.Get("/publications/{id}", func(w http.ResponseWriter, req *http.Request) {
		pub, err := loadOwnedPublication(w, req, repo, provider, chi.URLParam(req, "id"))
		if pub == nil {
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, serializePublication(*pub))
	})

	r.Patch("/publications/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		pub, err := loadOwnedPublication(w, req, repo, provider, id)
		if pub == nil {
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		var body struct {
			Persona *struct {
				Name      *string `json:"name"`
				AvatarURL *string `json:"avatarUrl"`
			} `json:"persona"`
			Capabilities []string `json:"capabilities"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if body.Persona != nil {
			name := pub.PersonaName
			if body.Persona.Name != nil {
				name = *body.Persona.Name
			}
			avatar := pub.PersonaAvatarURL
			if body.Persona.AvatarURL != nil {
				avatar = body.Persona.AvatarURL
			}
			if err := repo.UpdatePublicationPersona(
				req.Context(), provider, id, name, avatar,
			); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		if body.Capabilities != nil {
			if err := repo.UpdatePublicationCapabilities(
				req.Context(), provider, id, body.Capabilities,
			); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		updated, err := repo.GetPublication(req.Context(), provider, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, serializePublication(*updated))
	})

	r.Delete("/publications/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		pub, err := loadOwnedPublication(w, req, repo, provider, id)
		if pub == nil {
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := repo.MarkPublicationUnpublished(
			req.Context(), provider, id, time.Now().Unix(),
		); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"id": id, "status": "unpublished",
		})
	})

	r.Post("/start-a1", handleInstallProxyNotConfigured)
	r.Post("/credentials", handleInstallProxyNotConfigured)
	r.Post("/handoff-link", handleInstallProxyNotConfigured)
	r.Post("/personal-token", handleInstallProxyNotConfigured)

	r.Post("/publications/{id}/form-token", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		pub, err := loadOwnedPublication(w, req, repo, provider, id)
		if pub == nil {
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !isPendingPublicationStatus(pub.Status) {
			writeError(w, http.StatusConflict,
				"publication is '"+pub.Status+"'; cannot reissue form token")
			return
		}
		writeJSON(w, http.StatusOK, linearPublicationShell(*pub, origin))
	})

	if provider == store.ProviderLinear {
		r.Post("/publications", func(w http.ResponseWriter, req *http.Request) {
			uid := userID(req)
			var body struct {
				AgentID          string  `json:"agentId"`
				EnvironmentID    string  `json:"environmentId"`
				PersonaName      string  `json:"personaName"`
				PersonaAvatarURL *string `json:"personaAvatarUrl"`
				ReturnURL        string  `json:"returnUrl"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON body")
				return
			}
			if body.AgentID == "" || body.EnvironmentID == "" ||
				body.PersonaName == "" || body.ReturnURL == "" {
				writeError(w, http.StatusBadRequest,
					"agentId, environmentId, personaName, returnUrl required")
				return
			}
			id := "pub_" + strings.ReplaceAll(uuid.NewString(), "-", "")
			pub, err := repo.InsertPublicationShell(
				req.Context(), provider, id,
				store.NewPublicationShell{
					TenantID:         tenantID(req),
					UserID:           uid,
					AgentID:          body.AgentID,
					EnvironmentID:    body.EnvironmentID,
					PersonaName:      body.PersonaName,
					PersonaAvatarURL: body.PersonaAvatarURL,
					Capabilities:     []string{},
					ReturnURL:        body.ReturnURL,
				},
			)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, linearPublicationShell(*pub, origin))
		})

		r.Patch("/publications/{id}/credentials", func(w http.ResponseWriter, req *http.Request) {
			id := chi.URLParam(req, "id")
			pub, err := loadOwnedPublication(w, req, repo, provider, id)
			if pub == nil {
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			var body struct {
				ClientID      string  `json:"clientId"`
				ClientSecret  string  `json:"clientSecret"`
				WebhookSecret string  `json:"webhookSecret"`
				SigningSecret *string `json:"signingSecret"`
				ReturnURL     string  `json:"returnUrl"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON body")
				return
			}
			if body.ClientID == "" || body.ClientSecret == "" ||
				body.WebhookSecret == "" {
				writeError(w, http.StatusBadRequest,
					"clientId, clientSecret, webhookSecret required")
				return
			}
			if body.ReturnURL == "" {
				writeError(w, http.StatusBadRequest, "returnUrl required")
				return
			}
			if err := repo.SetPublicationCredentials(
				req.Context(), provider, id,
				store.PublicationCredentials{
					ClientID:      body.ClientID,
					ClientSecret:  body.ClientSecret,
					WebhookSecret: body.WebhookSecret,
					SigningSecret: body.SigningSecret,
				},
			); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			shell := linearPublicationShell(*pub, origin)
			installURL := origin + "/linear/oauth/pub/" + id + "/authorize"
			if linearHandler != nil {
				if signed, err := linearHandler.BuildInstallURL(id, body.ReturnURL); err == nil {
					installURL = signed
				}
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"install_url":     installURL,
				"publication_id":  id,
				"callback_url":    shell["callback_url"],
				"webhook_url":     shell["webhook_url"],
			})
		})

		mountLinearDispatchRuleRoutes(r, repo)
	} else {
		r.Post("/publications", handleInstallProxyNotConfigured)
	}
}

func mountLinearDispatchRuleRoutes(
	r chi.Router,
	repo *store.IntegrationRepo,
) {
	r.Get("/publications/{id}/dispatch-rules", func(w http.ResponseWriter, req *http.Request) {
		pubID := chi.URLParam(req, "id")
		pub, err := loadOwnedPublication(w, req, repo, store.ProviderLinear, pubID)
		if pub == nil {
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		list, err := repo.ListLinearDispatchRules(req.Context(), pubID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		rules := make([]map[string]any, 0, len(list))
		for _, rule := range list {
			rules = append(rules, serializeDispatchRule(rule))
		}
		writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
	})

	r.Post("/publications/{id}/dispatch-rules", func(w http.ResponseWriter, req *http.Request) {
		pubID := chi.URLParam(req, "id")
		pub, err := loadOwnedPublication(w, req, repo, store.ProviderLinear, pubID)
		if pub == nil {
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		var body dispatchRuleBody
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if !dispatchRuleHasFilter(body) {
			writeJSON(w, http.StatusBadRequest, map[string]any{
				"error": "at least one of filter_label, filter_states, " +
					"filter_project_id required",
				"hint": "An unfiltered rule would assign every active issue " +
					"in the workspace to the bot. Start with filter_label " +
					"(e.g. 'bot-ready') to scope to opted-in issues.",
			})
			return
		}
		maxConcurrent := 5
		if body.MaxConcurrent != nil {
			maxConcurrent = *body.MaxConcurrent
		}
		if maxConcurrent < 1 || maxConcurrent > 100 {
			writeError(w, http.StatusBadRequest,
				"max_concurrent must be 1..100")
			return
		}
		pollInterval := 600
		if body.PollIntervalSeconds != nil {
			pollInterval = *body.PollIntervalSeconds
		}
		if pollInterval < 60 || pollInterval > 86400 {
			writeError(w, http.StatusBadRequest,
				"poll_interval_seconds must be 60..86400")
			return
		}
		name := "Auto-pickup"
		if body.Name != nil && strings.TrimSpace(*body.Name) != "" {
			name = strings.TrimSpace(*body.Name)
		}
		enabled := true
		if body.Enabled != nil {
			enabled = *body.Enabled
		}
		id := "rule_" + strings.ReplaceAll(uuid.NewString(), "-", "")
		rule, err := repo.InsertLinearDispatchRule(req.Context(), id,
			store.NewLinearDispatchRule{
				TenantID:            pub.TenantID,
				PublicationID:       pubID,
				Name:                name,
				Enabled:             enabled,
				FilterLabel:         trimOptional(body.FilterLabel),
				FilterStates:        body.FilterStates,
				FilterProjectID:     trimOptional(body.FilterProjectID),
				MaxConcurrent:       maxConcurrent,
				PollIntervalSeconds: pollInterval,
			})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, serializeDispatchRule(*rule))
	})

	r.Patch("/publications/{id}/dispatch-rules/{ruleId}",
		func(w http.ResponseWriter, req *http.Request) {
			pubID := chi.URLParam(req, "id")
			ruleID := chi.URLParam(req, "ruleId")
			pub, err := loadOwnedPublication(
				w, req, repo, store.ProviderLinear, pubID,
			)
			if pub == nil {
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			existing, err := repo.GetLinearDispatchRule(req.Context(), ruleID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if existing == nil || existing.PublicationID != pubID {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			var body dispatchRuleBody
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, "invalid JSON body")
				return
			}
			patch := store.LinearDispatchRulePatch{
				Name:                body.Name,
				Enabled:             body.Enabled,
				MaxConcurrent:       body.MaxConcurrent,
				PollIntervalSeconds: body.PollIntervalSeconds,
			}
			if body.FilterLabel != nil {
				v := trimOptional(body.FilterLabel)
				patch.FilterLabel = &v
			}
			if body.FilterStates != nil {
				patch.FilterStates = &body.FilterStates
			}
			if body.FilterProjectID != nil {
				v := trimOptional(body.FilterProjectID)
				patch.FilterProjectID = &v
			}
			updated, err := repo.UpdateLinearDispatchRule(
				req.Context(), ruleID, patch,
			)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if updated == nil {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeJSON(w, http.StatusOK, serializeDispatchRule(*updated))
		})

	r.Delete("/publications/{id}/dispatch-rules/{ruleId}",
		func(w http.ResponseWriter, req *http.Request) {
			pubID := chi.URLParam(req, "id")
			ruleID := chi.URLParam(req, "ruleId")
			pub, err := loadOwnedPublication(
				w, req, repo, store.ProviderLinear, pubID,
			)
			if pub == nil {
				return
			}
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			existing, err := repo.GetLinearDispatchRule(req.Context(), ruleID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if existing == nil || existing.PublicationID != pubID {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			if err := repo.DeleteLinearDispatchRule(req.Context(), ruleID); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"id": ruleID, "status": "deleted",
			})
		})
}

type dispatchRuleBody struct {
	Name                *string  `json:"name"`
	Enabled             *bool    `json:"enabled"`
	FilterLabel         *string  `json:"filter_label"`
	FilterStates        []string `json:"filter_states"`
	FilterProjectID     *string  `json:"filter_project_id"`
	MaxConcurrent       *int     `json:"max_concurrent"`
	PollIntervalSeconds *int     `json:"poll_interval_seconds"`
}

func requireIntegrationUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if userID(r) == "" {
			writeError(w, http.StatusForbidden,
				"user-scoped endpoint: regenerate your API key "+
					"(legacy keys lack user_id) or sign in with a session cookie")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func handleInstallProxyNotConfigured(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusServiceUnavailable, "install proxy not configured")
}

func loadOwnedPublication(
	w http.ResponseWriter,
	req *http.Request,
	repo *store.IntegrationRepo,
	provider store.IntegrationProvider,
	id string,
) (*store.IntegrationPublication, error) {
	pub, err := repo.GetPublication(req.Context(), provider, id)
	if err != nil {
		return nil, err
	}
	if pub == nil || pub.UserID != userID(req) {
		writeError(w, http.StatusNotFound, "not found")
		return nil, nil
	}
	return pub, nil
}

func isPendingPublicationStatus(status string) bool {
	switch status {
	case "pending_setup", "credentials_filled", "awaiting_install":
		return true
	default:
		return false
	}
}

func linearPublicationShell(
	pub store.IntegrationPublication,
	origin string,
) map[string]any {
	returnURL := ""
	if pub.ReturnURL != nil {
		returnURL = *pub.ReturnURL
	}
	suggestedAvatar := pub.PersonaAvatarURL
	return map[string]any{
		"publication_id":       pub.ID,
		"callback_url":         origin + "/linear/oauth/pub/" + pub.ID + "/callback",
		"webhook_url":          origin + "/linear/webhook/pub/" + pub.ID,
		"suggested_app_name":   pub.PersonaName,
		"suggested_avatar_url": suggestedAvatar,
		"return_url":           returnURL,
	}
}

func serializeInstallation(item store.IntegrationInstallation) map[string]any {
	out := map[string]any{
		"id":             item.ID,
		"workspace_id":   item.WorkspaceID,
		"workspace_name": item.WorkspaceName,
		"install_kind":   item.InstallKind,
		"bot_user_id":    item.BotUserID,
		"bot_login":      item.BotUserID,
		"created_at":     item.CreatedAt,
	}
	if item.VaultID != nil {
		out["vault_id"] = *item.VaultID
	} else {
		out["vault_id"] = nil
	}
	return out
}

func serializePublications(list []store.IntegrationPublication) []map[string]any {
	out := make([]map[string]any, 0, len(list))
	for _, item := range list {
		out = append(out, serializePublication(item))
	}
	return out
}

func serializePublication(p store.IntegrationPublication) map[string]any {
	envID := ""
	if p.EnvironmentID != nil {
		envID = *p.EnvironmentID
	}
	var unpublished any
	if p.UnpublishedAt != nil {
		unpublished = *p.UnpublishedAt
	} else {
		unpublished = nil
	}
	caps := p.Capabilities
	if caps == nil {
		caps = []string{}
	}
	return map[string]any{
		"id":               p.ID,
		"user_id":          p.UserID,
		"agent_id":         p.AgentID,
		"installation_id":  p.InstallationID,
		"environment_id":   envID,
		"mode":             p.Mode,
		"status":           p.Status,
		"persona": map[string]any{
			"name":      p.PersonaName,
			"avatarUrl": p.PersonaAvatarURL,
		},
		"capabilities":         caps,
		"session_granularity":  p.SessionGranularity,
		"created_at":           p.CreatedAt,
		"unpublished_at":       unpublished,
	}
}

func serializeDispatchRule(r store.LinearDispatchRule) map[string]any {
	var lastPolled any
	if r.LastPolledAt != nil {
		lastPolled = *r.LastPolledAt
	}
	return map[string]any{
		"id":                    r.ID,
		"publication_id":        r.PublicationID,
		"name":                  r.Name,
		"enabled":               r.Enabled,
		"filter_label":          r.FilterLabel,
		"filter_states":         r.FilterStates,
		"filter_project_id":     r.FilterProjectID,
		"max_concurrent":        r.MaxConcurrent,
		"poll_interval_seconds": r.PollIntervalSeconds,
		"last_polled_at":        lastPolled,
		"created_at":            r.CreatedAt,
		"updated_at":            r.UpdatedAt,
	}
}

func dispatchRuleHasFilter(body dispatchRuleBody) bool {
	if body.FilterLabel != nil && strings.TrimSpace(*body.FilterLabel) != "" {
		return true
	}
	if len(body.FilterStates) > 0 {
		return true
	}
	if body.FilterProjectID != nil &&
		strings.TrimSpace(*body.FilterProjectID) != "" {
		return true
	}
	return false
}

func trimOptional(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	if v == "" {
		return nil
	}
	return &v
}

func integrationsGatewayOrigin() string {
	if v := os.Getenv("INTEGRATIONS_GATEWAY_ORIGIN"); v != "" {
		return strings.TrimRight(v, "/")
	}
	if v := os.Getenv("OMA_GATEWAY_ORIGIN"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "http://127.0.0.1:8787"
}
