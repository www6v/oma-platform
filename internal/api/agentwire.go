package api

import (
	"encoding/json"
	"fmt"

	"github.com/open-ma/oma-building/internal/store"
)

type agentRowMeta struct {
	CreatedAt  int64
	UpdatedAt  *int64
	ArchivedAt *int64
}

type omaEnvelope struct {
	AuxModel          json.RawMessage `json:"aux_model"`
	Harness           string          `json:"harness"`
	RuntimeBinding    json.RawMessage `json:"runtime_binding"`
	AppendablePrompts json.RawMessage `json:"appendable_prompts"`
}

type callableAgentEntry struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Version int    `json:"version"`
}

func parseModelField(raw json.RawMessage) (id, speed string, err error) {
	if len(raw) == 0 {
		return "", "", nil
	}
	trimmed := string(raw)
	if trimmed == "null" {
		return "", "", nil
	}
	var modelStr string
	if err := json.Unmarshal(raw, &modelStr); err == nil {
		if modelStr == "" {
			return "", "", nil
		}
		return modelStr, "standard", nil
	}
	var modelObj struct {
		ID    string `json:"id"`
		Speed string `json:"speed"`
	}
	if err := json.Unmarshal(raw, &modelObj); err != nil {
		return "", "", fmt.Errorf("model must be a string or object")
	}
	speed = modelObj.Speed
	if speed == "" {
		speed = "standard"
	}
	return modelObj.ID, speed, nil
}

func multiagentToCallableAgents(
	multiagent json.RawMessage,
) ([]callableAgentEntry, string, bool) {
	if len(multiagent) == 0 {
		return nil, "", false
	}
	trimmed := string(multiagent)
	if trimmed == "null" {
		return nil, "", true
	}
	var payload struct {
		Type   string          `json:"type"`
		Agents json.RawMessage `json:"agents"`
	}
	if err := json.Unmarshal(multiagent, &payload); err != nil {
		return nil, "multiagent must be an object", false
	}
	if payload.Type != "coordinator" {
		return nil, `multiagent.type must be "coordinator"`, false
	}
	var entries []json.RawMessage
	if err := json.Unmarshal(payload.Agents, &entries); err != nil {
		return nil, "multiagent.agents must be an array", false
	}
	out := make([]callableAgentEntry, 0, len(entries))
	for _, entry := range entries {
		var idStr string
		if err := json.Unmarshal(entry, &idStr); err == nil {
			out = append(out, callableAgentEntry{
				Type: "agent", ID: idStr, Version: 1,
			})
			continue
		}
		var obj struct {
			Type    string `json:"type"`
			ID      string `json:"id"`
			Version int    `json:"version"`
		}
		if err := json.Unmarshal(entry, &obj); err != nil {
			return nil, fmt.Sprintf(
				"multiagent.agents: invalid roster entry %s", string(entry),
			), false
		}
		if obj.Type == "self" {
			return nil, `multiagent.agents: {"type":"self"} is not yet supported`, false
		}
		if obj.Type == "agent" && obj.ID != "" {
			version := obj.Version
			if version < 1 {
				version = 1
			}
			out = append(out, callableAgentEntry{
				Type: "agent", ID: obj.ID, Version: version,
			})
			continue
		}
		return nil, fmt.Sprintf(
			"multiagent.agents: invalid roster entry %s", string(entry),
		), false
	}
	return out, "", true
}

func callableAgentsToJSON(entries []callableAgentEntry) json.RawMessage {
	if len(entries) == 0 {
		return nil
	}
	raw, err := json.Marshal(entries)
	if err != nil {
		return nil
	}
	return raw
}

func parseCallableAgents(raw json.RawMessage) []callableAgentEntry {
	if len(raw) == 0 || string(raw) == "null" {
		return []callableAgentEntry{}
	}
	var out []callableAgentEntry
	if err := json.Unmarshal(raw, &out); err != nil {
		return []callableAgentEntry{}
	}
	return out
}

func jsonArrayOrEmpty(raw json.RawMessage) []any {
	if len(raw) == 0 || string(raw) == "null" {
		return []any{}
	}
	var out []any
	if err := json.Unmarshal(raw, &out); err != nil {
		return []any{}
	}
	if out == nil {
		return []any{}
	}
	return out
}

func jsonObjectOrEmpty(raw json.RawMessage) map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func formatModelObject(id, speed string) map[string]any {
	if speed == "" {
		speed = "standard"
	}
	return map[string]any{"id": id, "speed": speed}
}

func formatAPIAgent(a *store.Agent) map[string]any {
	return formatAPIAgentConfig(&a.AgentConfig, agentRowMeta{
		CreatedAt:  a.CreatedAt,
		UpdatedAt:  a.UpdatedAt,
		ArchivedAt: a.ArchivedAt,
	})
}

func formatAPIAgentConfig(
	cfg *store.AgentConfig,
	meta agentRowMeta,
) map[string]any {
	sys := cfg.SystemPrompt
	if sys == "" {
		sys = cfg.System
	}
	modelSpeed := cfg.ModelSpeed
	if modelSpeed == "" {
		modelSpeed = "standard"
	}

	callable := parseCallableAgents(cfg.CallableAgents)
	var multiagent any
	if len(callable) > 0 {
		agents := make([]map[string]any, 0, len(callable))
		for _, entry := range callable {
			version := entry.Version
			if version < 1 {
				version = 1
			}
			agents = append(agents, map[string]any{
				"type": "agent", "id": entry.ID, "version": version,
			})
		}
		multiagent = map[string]any{
			"type": "coordinator", "agents": agents,
		}
	}

	out := map[string]any{
		"type":             "agent",
		"id":               cfg.ID,
		"name":             cfg.Name,
		"model":            formatModelObject(cfg.Model, modelSpeed),
		"system":           nullIfEmpty(sys),
		"description":      nullIfEmpty(cfg.Description),
		"skills":           jsonArrayOrEmpty(cfg.Skills),
		"mcp_servers":      jsonArrayOrEmpty(cfg.MCPServers),
		"multiagent":       multiagent,
		"callable_agents":  callable,
		"metadata":         jsonObjectOrEmpty(cfg.Metadata),
		"version":          cfg.Version,
	}
	if len(cfg.Tools) > 0 && string(cfg.Tools) != "null" {
		out["tools"] = json.RawMessage(cfg.Tools)
	}
	if meta.CreatedAt > 0 {
		out["created_at"] = formatISO(meta.CreatedAt)
	}
	if meta.UpdatedAt != nil {
		out["updated_at"] = formatISO(*meta.UpdatedAt)
	} else if meta.CreatedAt > 0 {
		out["updated_at"] = formatISO(meta.CreatedAt)
	}
	if meta.ArchivedAt != nil {
		out["archived_at"] = formatISO(*meta.ArchivedAt)
	} else {
		out["archived_at"] = nil
	}

	oma := map[string]any{}
	if cfg.AuxModel != "" {
		auxSpeed := cfg.AuxModelSpeed
		if auxSpeed == "" {
			auxSpeed = "standard"
		}
		oma["aux_model"] = formatModelObject(cfg.AuxModel, auxSpeed)
	}
	if cfg.Harness != "" {
		oma["harness"] = cfg.Harness
	}
	if len(cfg.RuntimeBinding) > 0 && string(cfg.RuntimeBinding) != "null" {
		oma["runtime_binding"] = json.RawMessage(cfg.RuntimeBinding)
	}
	appendable := jsonArrayOrEmpty(cfg.AppendablePrompts)
	if len(appendable) > 0 {
		oma["appendable_prompts"] = appendable
	}
	if len(oma) > 0 {
		out["_oma"] = oma
	}
	return out
}

func applyOmaEnvelope(cfg *store.AgentConfig, oma *omaEnvelope) {
	if oma == nil {
		return
	}
	if len(oma.AuxModel) > 0 {
		if string(oma.AuxModel) == "null" {
			cfg.AuxModel = ""
			cfg.AuxModelSpeed = ""
		} else {
			id, speed, err := parseModelField(oma.AuxModel)
			if err == nil {
				cfg.AuxModel = id
				cfg.AuxModelSpeed = speed
			}
		}
	}
	if oma.Harness != "" {
		cfg.Harness = oma.Harness
	}
	if len(oma.RuntimeBinding) > 0 {
		cfg.RuntimeBinding = oma.RuntimeBinding
	}
	if len(oma.AppendablePrompts) > 0 {
		cfg.AppendablePrompts = oma.AppendablePrompts
	}
}
