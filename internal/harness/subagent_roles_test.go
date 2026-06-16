package harness_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/store"
)

func TestResolveSubAgentsFromCallableAgents(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	agents := store.NewAgentRepo(db)
	ctx := context.Background()
	worker, err := agents.Create(ctx, store.CreateAgentInput{
		Name:         "worker",
		Model:        "faux/test",
		SystemPrompt: "worker prompt",
	})
	if err != nil {
		t.Fatal(err)
	}

	callable, err := json.Marshal([]map[string]any{
		{"type": "agent", "id": worker.ID, "version": 1},
	})
	if err != nil {
		t.Fatal(err)
	}

	parent := harness.AgentSnapshot{
		ID:             "agt_parent",
		Name:           "parent",
		Model:          "faux/test",
		CallableAgents: callable,
	}

	out, err := harness.ResolveSubAgents(ctx, agents, "default", parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("sub agents=%d", len(out))
	}
	if out[worker.ID].Name != "worker" {
		t.Fatalf("name=%q", out[worker.ID].Name)
	}
}

func TestResolveSubAgentsFromDefaultRoles(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	agents := store.NewAgentRepo(db)
	ctx := context.Background()
	worker, err := agents.Create(ctx, store.CreateAgentInput{
		Name:         "worker",
		Model:        "faux/test",
		SystemPrompt: "worker prompt",
	})
	if err != nil {
		t.Fatal(err)
	}

	meta, err := json.Marshal(map[string]any{
		"default_subagent_roles": map[string]string{
			"explore": worker.ID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	parent := harness.AgentSnapshot{
		ID:       "agt_parent",
		Name:     "parent",
		Model:    "faux/test",
		Metadata: meta,
	}

	out, err := harness.ResolveSubAgents(ctx, agents, "default", parent)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("sub agents=%d", len(out))
	}
	snap := out[worker.ID]
	var parsed map[string]any
	if err := json.Unmarshal(snap.Metadata, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["subagent_role"] != "explore" {
		t.Fatalf("metadata=%v", parsed)
	}
}

func TestResolveSubAgentsMergesExplicitCallableWithRoles(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	agents := store.NewAgentRepo(db)
	ctx := context.Background()
	worker, err := agents.Create(ctx, store.CreateAgentInput{
		Name:         "worker",
		Model:        "faux/test",
		SystemPrompt: "worker prompt",
	})
	if err != nil {
		t.Fatal(err)
	}

	callable, err := json.Marshal([]map[string]any{
		{"type": "agent", "id": worker.ID, "version": 1},
	})
	if err != nil {
		t.Fatal(err)
	}
	meta, err := json.Marshal(map[string]any{
		"default_subagent_roles": []map[string]string{
			{"id": worker.ID, "role": "plan"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	parent := harness.AgentSnapshot{
		ID:             "agt_parent",
		Name:           "parent",
		Model:          "faux/test",
		CallableAgents: callable,
		Metadata:       meta,
	}

	out, err := harness.ResolveSubAgents(ctx, agents, "default", parent)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out[worker.ID].Metadata, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["subagent_role"] != "plan" {
		t.Fatalf("metadata=%v", parsed)
	}
}
