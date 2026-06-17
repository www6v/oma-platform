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

type agentGetter interface {
	Get(ctx context.Context, tenantID, id string) (*store.Agent, error)
}

func parseCallableAgentRefs(raw json.RawMessage) []callableAgentRef {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var refs []callableAgentRef
	if err := json.Unmarshal(raw, &refs); err != nil {
		return nil
	}
	return refs
}

// ResolveSubAgents loads callable agent snapshots for a parent turn.
func ResolveSubAgents(
	ctx context.Context,
	tenantID string,
	parent AgentSnapshot,
	agents agentGetter,
) (map[string]AgentSnapshot, error) {
	refs := parseCallableAgentRefs(parent.CallableAgents)
	if len(refs) == 0 {
		return nil, nil
	}
	out := make(map[string]AgentSnapshot, len(refs))
	for _, ref := range refs {
		if ref.ID == "" {
			continue
		}
		row, err := agents.Get(ctx, tenantID, ref.ID)
		if err != nil {
			return nil, fmt.Errorf("callable agent %q: %w", ref.ID, err)
		}
		if row == nil {
			return nil, fmt.Errorf("callable agent %q not found", ref.ID)
		}
		out[ref.ID] = AgentSnapshotFromConfig(row.AgentConfig)
	}
	return out, nil
}
