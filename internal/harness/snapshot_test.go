package harness_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/store"
)

func TestAgentSnapshotFromRawUsesSystemField(t *testing.T) {
	raw, err := json.Marshal(map[string]any{
		"id":     "agt_1",
		"name":   "demo",
		"model":  "faux/test",
		"system": "hello system",
	})
	if err != nil {
		t.Fatal(err)
	}
	snap, err := harness.AgentSnapshotFromRaw(raw)
	if err != nil {
		t.Fatal(err)
	}
	if snap.SystemPrompt != "hello system" {
		t.Fatalf("system_prompt=%q", snap.SystemPrompt)
	}
}

func TestResolveSubAgents(t *testing.T) {
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

	out, err := harness.ResolveSubAgents(ctx, agents, "default", callable)
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
