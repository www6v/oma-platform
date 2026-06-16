package harness

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/open-ma/oma-building/internal/store"
)

type callableAgentRef struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Version int    `json:"version"`
}

// AgentSnapshotFromConfig maps a store agent config to the harness DTO.
func AgentSnapshotFromConfig(cfg store.AgentConfig) AgentSnapshot {
	sys := cfg.SystemPrompt
	if sys == "" {
		sys = cfg.System
	}
	return AgentSnapshot{
		ID:             cfg.ID,
		Name:           cfg.Name,
		Model:          cfg.Model,
		AuxModel:       cfg.AuxModel,
		SystemPrompt:   sys,
		Description:    cfg.Description,
		Tools:          cfg.Tools,
		MCPServers:     cfg.MCPServers,
		CallableAgents: cfg.CallableAgents,
		Metadata:       cfg.Metadata,
		Version:        cfg.Version,
	}
}

// AgentSnapshotFromRaw unmarshals a session agent snapshot blob.
func AgentSnapshotFromRaw(raw json.RawMessage) (AgentSnapshot, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return AgentSnapshot{}, fmt.Errorf("empty agent snapshot")
	}
	var cfg store.AgentConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return AgentSnapshot{}, fmt.Errorf("parse agent snapshot: %w", err)
	}
	return AgentSnapshotFromConfig(cfg), nil
}

// ResolveSubAgents loads callable agent configs for a turn. When the parent
// agent metadata defines default_subagent_roles, those entries merge with
// callable_agents and tag each sub-agent with metadata.subagent_role.
func ResolveSubAgents(
	ctx context.Context,
	repo *store.AgentRepo,
	tenantID string,
	parent AgentSnapshot,
) (map[string]AgentSnapshot, error) {
	refs, rolesByID, err := mergeCallableWithRoleDefaults(
		parent.CallableAgents,
		parent.Metadata,
	)
	if err != nil {
		return nil, err
	}
	if len(refs) == 0 {
		return map[string]AgentSnapshot{}, nil
	}
	out := make(map[string]AgentSnapshot, len(refs))
	for _, ref := range refs {
		if ref.ID == "" {
			continue
		}
		agent, err := repo.Get(ctx, tenantID, ref.ID)
		if err != nil {
			return nil, err
		}
		if agent == nil {
			return nil, fmt.Errorf("callable agent %q not found", ref.ID)
		}
		snap := AgentSnapshotFromConfig(agent.AgentConfig)
		out[ref.ID] = tagSubagentRole(snap, rolesByID[ref.ID])
	}
	return out, nil
}

func parseCallableAgentRefs(raw json.RawMessage) ([]callableAgentRef, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var refs []callableAgentRef
	if err := json.Unmarshal(raw, &refs); err != nil {
		return nil, fmt.Errorf("parse callable_agents: %w", err)
	}
	return refs, nil
}
