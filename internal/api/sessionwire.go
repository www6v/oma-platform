package api

import (
	"encoding/json"
	"fmt"

	"github.com/open-ma/oma-building/internal/store"
)

func parseSessionAgentRef(raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("agent is required")
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		if asString == "" {
			return "", fmt.Errorf("agent is required")
		}
		return asString, nil
	}
	var asObj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &asObj); err != nil {
		return "", fmt.Errorf("invalid agent")
	}
	if asObj.ID == "" {
		return "", fmt.Errorf("agent is required")
	}
	return asObj.ID, nil
}

func snapshotToSessionAgent(
	agentID string,
	version int,
	snapshot json.RawMessage,
) map[string]any {
	if version < 1 {
		version = 1
	}
	if len(snapshot) == 0 || string(snapshot) == "null" {
		return map[string]any{
			"type": "agent", "id": agentID, "version": version,
		}
	}
	var cfg store.AgentConfig
	if err := json.Unmarshal(snapshot, &cfg); err != nil {
		return map[string]any{
			"type": "agent", "id": agentID, "version": version,
		}
	}
	cfg.ID = agentID
	if cfg.Version > 0 {
		version = cfg.Version
	}
	cfg.Version = version

	full := formatAPIAgentConfig(&cfg, agentRowMeta{})
	delete(full, "created_at")
	delete(full, "updated_at")
	delete(full, "archived_at")
	delete(full, "metadata")
	if oma, ok := full["_oma"].(map[string]any); ok {
		delete(oma, "harness")
		delete(oma, "runtime_binding")
		delete(oma, "aux_model")
		if len(oma) == 0 {
			delete(full, "_oma")
		}
	}
	return full
}

func formatAPISession(s *store.Session) map[string]any {
	version := s.AgentVersion
	if version < 1 {
		version = 1
	}
	var title any
	if s.Title != "" {
		title = s.Title
	}
	out := map[string]any{
		"type":            "session",
		"id":              s.ID,
		"agent_id":        s.AgentID,
		"agent": snapshotToSessionAgent(
			s.AgentID, version, s.AgentSnapshot,
		),
		"environment_id":  s.EnvironmentID,
		"title":           title,
		"status":          s.Status,
		"created_at":      s.CreatedAt,
		"vault_ids":       []any{},
		"metadata":        map[string]any{},
		"resources":       []any{},
		"outcome_evaluations": []any{},
		"usage":           map[string]any{},
		"stats":           map[string]any{},
	}
	if s.UpdatedAt != nil {
		out["updated_at"] = *s.UpdatedAt
	}
	if s.ArchivedAt != nil {
		out["archived_at"] = *s.ArchivedAt
	}
	return out
}
