package harness

import (
	"encoding/json"
	"fmt"
)

type subagentRoleEntry struct {
	ID   string `json:"id"`
	Role string `json:"role"`
}

// mergeCallableWithRoleDefaults combines explicit callable_agents refs with
// metadata.default_subagent_roles. When callable_agents is empty, roles
// alone define the delegation roster. Each resolved sub-agent receives
// metadata.subagent_role for the harness to apply role system prompts.
func mergeCallableWithRoleDefaults(
	callable json.RawMessage,
	metadata json.RawMessage,
) ([]callableAgentRef, map[string]string, error) {
	rolesByID, err := parseDefaultSubagentRoles(metadata)
	if err != nil {
		return nil, nil, err
	}

	refs, err := parseCallableAgentRefs(callable)
	if err != nil {
		return nil, nil, err
	}

	if len(refs) == 0 && len(rolesByID) > 0 {
		refs = make([]callableAgentRef, 0, len(rolesByID))
		for id := range rolesByID {
			refs = append(refs, callableAgentRef{Type: "agent", ID: id, Version: 1})
		}
	}

	roleForRef := make(map[string]string, len(refs))
	for _, ref := range refs {
		if ref.ID == "" {
			continue
		}
		if role, ok := rolesByID[ref.ID]; ok {
			roleForRef[ref.ID] = role
		}
	}
	return refs, roleForRef, nil
}

func parseDefaultSubagentRoles(metadata json.RawMessage) (map[string]string, error) {
	if len(metadata) == 0 || string(metadata) == "null" {
		return map[string]string{}, nil
	}
	var meta map[string]json.RawMessage
	if err := json.Unmarshal(metadata, &meta); err != nil {
		return nil, fmt.Errorf("parse agent metadata: %w", err)
	}
	raw, ok := meta["default_subagent_roles"]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return map[string]string{}, nil
	}

	out := map[string]string{}

	// Array form: [{"id":"agt_x","role":"explore"}, ...]
	var entries []subagentRoleEntry
	if err := json.Unmarshal(raw, &entries); err == nil && len(entries) > 0 {
		for _, e := range entries {
			if e.ID == "" || e.Role == "" {
				continue
			}
			out[e.ID] = e.Role
		}
		return out, nil
	}

	// Object form: {"explore":"agt_x","plan":"agt_y"}
	var byRole map[string]string
	if err := json.Unmarshal(raw, &byRole); err != nil {
		return nil, fmt.Errorf("parse default_subagent_roles: %w", err)
	}
	for role, id := range byRole {
		if id == "" || role == "" {
			continue
		}
		out[id] = role
	}
	return out, nil
}

func tagSubagentRole(snap AgentSnapshot, role string) AgentSnapshot {
	if role == "" {
		return snap
	}
	meta := map[string]any{}
	if len(snap.Metadata) > 0 && string(snap.Metadata) != "null" {
		_ = json.Unmarshal(snap.Metadata, &meta)
	}
	meta["subagent_role"] = role
	encoded, err := json.Marshal(meta)
	if err != nil {
		return snap
	}
	snap.Metadata = encoded
	return snap
}
