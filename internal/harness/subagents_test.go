package harness_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/store"
)

type stubAgentGetter struct {
	agents map[string]store.AgentConfig
}

func (s stubAgentGetter) Get(
	_ context.Context,
	_, id string,
) (*store.Agent, error) {
	cfg, ok := s.agents[id]
	if !ok {
		return nil, nil
	}
	return &store.Agent{AgentConfig: cfg}, nil
}

func TestResolveSubAgentsFromCallableAgents(t *testing.T) {
	callable, err := json.Marshal([]map[string]any{
		{"type": "agent", "id": "agt_worker", "version": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	parent := harness.AgentSnapshot{
		ID:             "agt_coord",
		CallableAgents: callable,
	}
	getter := stubAgentGetter{
		agents: map[string]store.AgentConfig{
			"agt_worker": {
				ID:   "agt_worker",
				Name: "subagent-ui-worker",
			},
		},
	}
	out, err := harness.ResolveSubAgents(
		context.Background(),
		"default",
		parent,
		getter,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("sub_agents=%v", out)
	}
	if out["agt_worker"].Name != "subagent-ui-worker" {
		t.Fatalf("name=%q", out["agt_worker"].Name)
	}
}

func TestResolveSubAgentsMissingAgent(t *testing.T) {
	callable, err := json.Marshal([]map[string]any{
		{"type": "agent", "id": "missing", "version": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	parent := harness.AgentSnapshot{
		ID:             "agt_coord",
		CallableAgents: callable,
	}
	_, err = harness.ResolveSubAgents(
		context.Background(),
		"default",
		parent,
		stubAgentGetter{agents: map[string]store.AgentConfig{}},
	)
	if err == nil {
		t.Fatal("expected error for missing callable agent")
	}
}
