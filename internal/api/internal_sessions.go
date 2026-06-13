package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/store"
)

type internalCreateSessionBody struct {
	Action                   string             `json:"action"`
	UserID                   string             `json:"userId"`
	AgentID                  string             `json:"agentId"`
	EnvironmentID            string             `json:"environmentId"`
	VaultIDs                 []string           `json:"vaultIds"`
	MCPServers               []internalMCPServer  `json:"mcpServers"`
	Metadata                 map[string]any     `json:"metadata"`
	InitialEvent             *json.RawMessage   `json:"initialEvent"`
	AdditionalSystemPrompt   string             `json:"additionalSystemPrompt"`
}

type internalResumeSessionBody struct {
	UserID string          `json:"userId"`
	Event  json.RawMessage `json:"event"`
}

func handleInternalCreateSession(deps internalDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Sessions == nil {
			writeError(w, http.StatusServiceUnavailable, "sessions not configured")
			return
		}
		var body internalCreateSessionBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if body.Action != "create" {
			writeError(w, http.StatusBadRequest, "unknown action")
			return
		}
		if body.UserID == "" || body.AgentID == "" || body.EnvironmentID == "" {
			writeError(w, http.StatusBadRequest, "userId, agentId, environmentId required")
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

		sess, err := deps.Sessions.sessions.Create(r.Context(), store.CreateSessionInput{
			TenantID:      tenantID,
			AgentID:       body.AgentID,
			EnvironmentID: body.EnvironmentID,
		})
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "agent or environment not found in tenant")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}

		if len(body.MCPServers) > 0 || body.AdditionalSystemPrompt != "" {
			snapshot, snapErr := augmentAgentSnapshot(
				sess.AgentSnapshot,
				body.MCPServers,
				body.AdditionalSystemPrompt,
			)
			if snapErr != nil {
				writeError(w, http.StatusInternalServerError, snapErr.Error())
				return
			}
			if err := deps.Sessions.sessions.UpdateAgentSnapshot(
				r.Context(), tenantID, sess.ID, snapshot,
			); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			sess.AgentSnapshot = snapshot
		}

		deps.Sessions.registerMachine(sess)

		if body.InitialEvent != nil && len(*body.InitialEvent) > 0 {
			if err := deps.Sessions.registry.EnqueueEvents(
				r.Context(),
				sess.ID,
				[]json.RawMessage{*body.InitialEvent},
				true,
				false,
				nil,
			); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}

		writeJSON(w, http.StatusOK, map[string]string{"sessionId": sess.ID})
	}
}

func handleInternalSessionEvents(deps internalDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Sessions == nil {
			writeError(w, http.StatusServiceUnavailable, "sessions not configured")
			return
		}
		sessionID := chi.URLParam(r, "id")
		var body internalResumeSessionBody
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if body.UserID == "" || len(body.Event) == 0 {
			writeError(w, http.StatusBadRequest, "userId and event required")
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

		sess, err := deps.Sessions.sessions.Get(r.Context(), tenantID, sessionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if sess == nil {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}

		deps.Sessions.registerMachine(sess)
		if err := deps.Sessions.registry.EnqueueEvents(
			r.Context(),
			sessionID,
			[]json.RawMessage{body.Event},
			true,
			false,
			nil,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	}
}

func handleInternalGetSession(deps internalDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Sessions == nil {
			writeError(w, http.StatusServiceUnavailable, "sessions not configured")
			return
		}
		sessionID := chi.URLParam(r, "id")
		sess, err := deps.Sessions.sessions.GetByID(r.Context(), sessionID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if sess == nil {
			writeError(w, http.StatusNotFound, "session not found")
			return
		}
		writeJSON(w, http.StatusOK, formatAPISession(sess))
	}
}

func resolveInternalTenantID(
	ctx context.Context,
	tenants *store.TenantRepo,
	userID string,
) (string, error) {
	if userID == "" {
		return "", nil
	}
	if tenants == nil {
		return "default", nil
	}
	return tenants.DefaultTenantForUser(ctx, userID)
}
