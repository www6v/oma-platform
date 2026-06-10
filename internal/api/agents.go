package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/store"
)

type agentWriteBody struct {
	Name         string          `json:"name"`
	Model        json.RawMessage `json:"model"`
	System       string          `json:"system"`
	SystemPrompt string          `json:"system_prompt"`
	Description  string          `json:"description"`
	Tools        json.RawMessage `json:"tools"`
	MCPServers   json.RawMessage `json:"mcp_servers"`
	Skills       json.RawMessage `json:"skills"`
	CallableAgents json.RawMessage `json:"callable_agents"`
	Multiagent   json.RawMessage `json:"multiagent"`
	Metadata     json.RawMessage `json:"metadata"`
	Harness      string          `json:"harness"`
	OMA          *omaEnvelope    `json:"_oma"`
}

type agentPatchBody struct {
	Name           *string          `json:"name"`
	Model          json.RawMessage  `json:"model"`
	System         *string          `json:"system"`
	SystemPrompt   *string          `json:"system_prompt"`
	Description    *string          `json:"description"`
	Tools          *json.RawMessage `json:"tools"`
	MCPServers     *json.RawMessage `json:"mcp_servers"`
	Skills         *json.RawMessage `json:"skills"`
	CallableAgents *json.RawMessage `json:"callable_agents"`
	Multiagent     json.RawMessage  `json:"multiagent"`
	Metadata       *json.RawMessage `json:"metadata"`
	Harness        *string          `json:"harness"`
	OMA            *omaEnvelope     `json:"_oma"`
}

func buildCreateAgentInput(
	tenant string,
	body agentWriteBody,
) (store.CreateAgentInput, string) {
	sys := body.SystemPrompt
	if sys == "" {
		sys = body.System
	}
	modelID, modelSpeed, err := parseModelField(body.Model)
	if err != nil {
		return store.CreateAgentInput{}, err.Error()
	}
	hasRuntime := body.OMA != nil &&
		len(body.OMA.RuntimeBinding) > 0 &&
		string(body.OMA.RuntimeBinding) != "null"
	if modelID == "" && !hasRuntime {
		return store.CreateAgentInput{}, "model is required"
	}
	if err := validateAgentTools(body.Tools); err != nil {
		return store.CreateAgentInput{}, err.Error()
	}

	callable := body.CallableAgents
	if len(body.Multiagent) > 0 {
		entries, msg, set := multiagentToCallableAgents(body.Multiagent)
		if msg != "" {
			return store.CreateAgentInput{}, msg
		}
		if set {
			callable = callableAgentsToJSON(entries)
		}
	}

	input := store.CreateAgentInput{
		TenantID:       tenant,
		Name:           body.Name,
		Model:          modelID,
		ModelSpeed:     modelSpeed,
		SystemPrompt:   sys,
		Description:    body.Description,
		Tools:          body.Tools,
		MCPServers:     body.MCPServers,
		Skills:         body.Skills,
		CallableAgents: callable,
		Metadata:       body.Metadata,
		Harness:        body.Harness,
	}
	if body.OMA != nil {
		if body.OMA.Harness != "" {
			input.Harness = body.OMA.Harness
		}
		if len(body.OMA.RuntimeBinding) > 0 {
			input.RuntimeBinding = body.OMA.RuntimeBinding
		}
		if len(body.OMA.AppendablePrompts) > 0 {
			input.AppendablePrompts = body.OMA.AppendablePrompts
		}
		if len(body.OMA.AuxModel) > 0 && string(body.OMA.AuxModel) != "null" {
			auxID, auxSpeed, err := parseModelField(body.OMA.AuxModel)
			if err != nil {
				return store.CreateAgentInput{}, err.Error()
			}
			input.AuxModel = auxID
			input.AuxModelSpeed = auxSpeed
		}
	}
	return input, ""
}

func buildUpdateAgentInput(body agentPatchBody) (store.UpdateAgentInput, string) {
	patch := store.UpdateAgentInput{}
	if body.Name != nil {
		patch.Name = body.Name
	}
	if len(body.Model) > 0 {
		modelID, modelSpeed, err := parseModelField(body.Model)
		if err != nil {
			return store.UpdateAgentInput{}, err.Error()
		}
		patch.Model = &modelID
		patch.ModelSpeed = &modelSpeed
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
			return store.UpdateAgentInput{}, err.Error()
		}
		patch.Tools = *body.Tools
		patch.ToolsSet = true
	}
	if body.MCPServers != nil {
		patch.MCPServers = *body.MCPServers
		patch.MCPServersSet = true
	}
	if body.Skills != nil {
		patch.Skills = *body.Skills
		patch.SkillsSet = true
	}
	if len(body.Multiagent) > 0 {
		entries, msg, set := multiagentToCallableAgents(body.Multiagent)
		if msg != "" {
			return store.UpdateAgentInput{}, msg
		}
		if set {
			patch.CallableAgents = callableAgentsToJSON(entries)
			patch.CallableAgentsSet = true
		}
	} else if body.CallableAgents != nil {
		patch.CallableAgents = *body.CallableAgents
		patch.CallableAgentsSet = true
	}
	if body.Metadata != nil {
		patch.Metadata = *body.Metadata
		patch.MetadataSet = true
	}
	if body.Harness != nil {
		patch.Harness = body.Harness
	}
	if body.OMA != nil {
		if body.OMA.Harness != "" {
			h := body.OMA.Harness
			patch.Harness = &h
		}
		if len(body.OMA.RuntimeBinding) > 0 {
			patch.RuntimeBinding = body.OMA.RuntimeBinding
			patch.RuntimeBindingSet = true
		}
		if len(body.OMA.AppendablePrompts) > 0 {
			patch.AppendablePrompts = body.OMA.AppendablePrompts
			patch.AppendablePromptsSet = true
		}
		if len(body.OMA.AuxModel) > 0 {
			if string(body.OMA.AuxModel) == "null" {
				empty := ""
				patch.AuxModel = &empty
				patch.AuxModelSpeed = &empty
			} else {
				auxID, auxSpeed, err := parseModelField(body.OMA.AuxModel)
				if err != nil {
					return store.UpdateAgentInput{}, err.Error()
				}
				patch.AuxModel = &auxID
				patch.AuxModelSpeed = &auxSpeed
			}
		}
	}
	return patch, ""
}

func mountAgentRoutes(r chi.Router, agents *store.AgentRepo) {
	r.Post("/", func(w http.ResponseWriter, req *http.Request) {
		var body agentWriteBody
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if body.Name == "" {
			writeError(w, http.StatusBadRequest, "name is required")
			return
		}
		input, errMsg := buildCreateAgentInput(tenantID(req), body)
		if errMsg != "" {
			writeError(w, http.StatusBadRequest, errMsg)
			return
		}
		agent, err := agents.Create(req.Context(), input)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, formatAPIAgent(agent))
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
		out := make([]map[string]any, 0, len(page.Items))
		for _, a := range page.Items {
			out = append(out, formatAPIAgent(a))
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
		out := make([]map[string]any, 0, len(versions))
		for _, v := range versions {
			out = append(out, formatAPIAgentConfig(&v.Snapshot, agentRowMeta{}))
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
		writeJSON(w, http.StatusOK, formatAPIAgentConfig(&snap.Snapshot, agentRowMeta{}))
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
		writeJSON(w, http.StatusOK, formatAPIAgent(agent))
	})

	updateAgent := func(w http.ResponseWriter, req *http.Request) {
		id := chi.URLParam(req, "id")
		var body agentPatchBody
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		patch, errMsg := buildUpdateAgentInput(body)
		if errMsg != "" {
			writeError(w, http.StatusBadRequest, errMsg)
			return
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
		writeJSON(w, http.StatusOK, formatAPIAgent(agent))
	}
	r.Patch("/{id}", updateAgent)
	r.Put("/{id}", updateAgent)
	r.Post("/{id}", updateAgent)

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
		writeJSON(w, http.StatusOK, formatAPIAgent(agent))
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
