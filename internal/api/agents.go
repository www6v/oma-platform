package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/store"
)

type agentResponse struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Model        string          `json:"model"`
	SystemPrompt string          `json:"system_prompt,omitempty"`
	System       string          `json:"system,omitempty"`
	Description  string          `json:"description,omitempty"`
	Tools        json.RawMessage `json:"tools,omitempty"`
	Version      int    `json:"version"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    *int64 `json:"updated_at,omitempty"`
	ArchivedAt   *int64 `json:"archived_at,omitempty"`
}

type agentVersionResponse struct {
	AgentID   string          `json:"agent_id"`
	Version   int             `json:"version"`
	Snapshot  agentResponse   `json:"snapshot"`
	CreatedAt int64           `json:"created_at"`
}

func formatAgent(a *store.Agent) agentResponse {
	sys := a.SystemPrompt
	return agentResponse{
		ID:           a.ID,
		Name:         a.Name,
		Model:        a.Model,
		SystemPrompt: sys,
		System:       sys,
		Description:  a.Description,
		Tools:        a.Tools,
		Version:      a.Version,
		CreatedAt:    a.CreatedAt,
		UpdatedAt:    a.UpdatedAt,
		ArchivedAt:   a.ArchivedAt,
	}
}

func formatAgentVersion(v store.AgentVersion) agentVersionResponse {
	return agentVersionResponse{
		AgentID: v.AgentID,
		Version: v.Version,
		Snapshot: agentResponse{
			ID:           v.Snapshot.ID,
			Name:         v.Snapshot.Name,
			Model:        v.Snapshot.Model,
			SystemPrompt: v.Snapshot.SystemPrompt,
			System:       v.Snapshot.SystemPrompt,
			Description:  v.Snapshot.Description,
			Tools:        v.Snapshot.Tools,
			Version:      v.Snapshot.Version,
		},
		CreatedAt: v.CreatedAt,
	}
}

type createAgentRequest struct {
	Name         string          `json:"name"`
	Model        string          `json:"model"`
	System       string          `json:"system"`
	SystemPrompt string          `json:"system_prompt"`
	Description  string          `json:"description"`
	Tools        json.RawMessage `json:"tools"`
}

type patchAgentRequest struct {
	Name         *string          `json:"name"`
	Model        *string          `json:"model"`
	System       *string          `json:"system"`
	SystemPrompt *string          `json:"system_prompt"`
	Description  *string          `json:"description"`
	Tools        *json.RawMessage `json:"tools"`
}

func mountAgentRoutes(r chi.Router, agents *store.AgentRepo) {
	r.Post("/", func(w http.ResponseWriter, req *http.Request) {
		var body createAgentRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		sys := body.SystemPrompt
		if sys == "" {
			sys = body.System
		}
		if err := validateAgentTools(body.Tools); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		agent, err := agents.Create(req.Context(), store.CreateAgentInput{
			TenantID:     tenantID(req),
			Name:         body.Name,
			Model:        body.Model,
			SystemPrompt: sys,
			Description:  body.Description,
			Tools:        body.Tools,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, formatAgent(agent))
	})

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		params := parseAgentListParams(req)
		if params.Err != "" {
			writeError(w, http.StatusBadRequest, params.Err)
			return
		}
		page, err := agents.ListPage(req.Context(), params.Query)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]agentResponse, 0, len(page.Items))
		for _, a := range page.Items {
			out = append(out, formatAgent(a))
		}
		writeListPage(w, out, page.NextCursor)
	})

	r.Get("/{id}/versions", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		versions, err := agents.ListVersions(req.Context(), tenantID(req), id)
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]agentVersionResponse, 0, len(versions))
		for _, v := range versions {
			out = append(out, formatAgentVersion(v))
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": out})
	})

	r.Get("/{id}/versions/{version}", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		rawVersion := chi.URLParam(req, "version")
		version, err := strconv.Atoi(rawVersion)
		if err != nil || version < 1 {
			writeError(w, http.StatusBadRequest, "invalid version")
			return
		}
		snap, err := agents.GetVersion(req.Context(), tenantID(req), id, version)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if snap == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeJSON(w, http.StatusOK, formatAgentVersion(*snap))
	})

	r.Get("/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		agent, err := agents.Get(req.Context(), tenantID(req), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if agent == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeJSON(w, http.StatusOK, formatAgent(agent))
	})

	r.Patch("/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		var body patchAgentRequest
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		patch := store.UpdateAgentInput{
			Name:  body.Name,
			Model: body.Model,
		}
		if body.SystemPrompt != nil {
			patch.SystemPrompt = body.SystemPrompt
		} else if body.System != nil {
			patch.SystemPrompt = body.System
		}
		if body.Description != nil {
			patch.Description = body.Description
		}
		if body.Tools != nil {
			if err := validateAgentTools(*body.Tools); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return
			}
			patch.Tools = *body.Tools
			patch.ToolsSet = true
		}
		agent, err := agents.Update(req.Context(), tenantID(req), id, patch)
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "not found")
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
		writeJSON(w, http.StatusOK, formatAgent(agent))
	})

	r.Post("/{id}/archive", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		agent, err := agents.Archive(req.Context(), tenantID(req), id)
		if err == store.ErrNotFound {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, formatAgent(agent))
	})

	r.Delete("/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		agent, err := agents.Get(req.Context(), tenantID(req), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if agent == nil {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		has, err := agents.HasActiveSessions(req.Context(), tenantID(req), id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if has {
			writeError(
				w, http.StatusConflict,
				"Cannot delete agent with active sessions. "+
					"Archive or delete sessions first.",
			)
			return
		}
		if err := agents.Delete(req.Context(), tenantID(req), id); err != nil {
			if err == store.ErrNotFound {
				writeError(w, http.StatusNotFound, "not found")
				return
			}
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"type": "agent_deleted",
			"id":   id,
		})
	})
}
