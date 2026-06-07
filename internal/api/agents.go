package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/store"
)

type agentResponse struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Model        string `json:"model"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	System       string `json:"system,omitempty"`
	Version      int    `json:"version"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    *int64 `json:"updated_at,omitempty"`
	ArchivedAt   *int64 `json:"archived_at,omitempty"`
}

func formatAgent(a *store.Agent) agentResponse {
	sys := a.SystemPrompt
	return agentResponse{
		ID:           a.ID,
		Name:         a.Name,
		Model:        a.Model,
		SystemPrompt: sys,
		System:       sys,
		Version:      a.Version,
		CreatedAt:    a.CreatedAt,
		UpdatedAt:    a.UpdatedAt,
		ArchivedAt:   a.ArchivedAt,
	}
}

type createAgentRequest struct {
	Name         string `json:"name"`
	Model        string `json:"model"`
	System       string `json:"system"`
	SystemPrompt string `json:"system_prompt"`
}

type patchAgentRequest struct {
	Name         *string `json:"name"`
	Model        *string `json:"model"`
	System       *string `json:"system"`
	SystemPrompt *string `json:"system_prompt"`
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
		agent, err := agents.Create(req.Context(), store.CreateAgentInput{
			TenantID:     defaultTenant,
			Name:         body.Name,
			Model:        body.Model,
			SystemPrompt: sys,
		})
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, formatAgent(agent))
	})

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		includeArchived := req.URL.Query().Get("include_archived") == "true"
		list, err := agents.List(req.Context(), defaultTenant, includeArchived)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]agentResponse, 0, len(list))
		for _, a := range list {
			out = append(out, formatAgent(a))
		}
		writeJSON(w, http.StatusOK, map[string]any{"data": out})
	})

	r.Get("/{id}", func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		agent, err := agents.Get(req.Context(), defaultTenant, id)
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
		agent, err := agents.Update(req.Context(), defaultTenant, id, patch)
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
		agent, err := agents.Archive(req.Context(), defaultTenant, id)
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
}
