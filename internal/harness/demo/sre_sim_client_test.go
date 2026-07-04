package demo

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/open-ma/oma-building/internal/harness"
)

func TestSreSimulatingClientTurnSequence(t *testing.T) {
	sim := &SreSimulatingClient{}
	ctx := context.Background()

	skillRaw, _ := json.Marshal(map[string]any{
		"type": "skill",
		"name": "incident-runbooks",
	})
	resLog, _ := json.Marshal(map[string]any{
		"type":       "file",
		"mount_path": sreLogMount,
	})
	resManifest, _ := json.Marshal(map[string]any{
		"type":       "file",
		"mount_path": sreManifestMount,
	})
	resRunbook, _ := json.Marshal(map[string]any{
		"type":       "file",
		"mount_path": sreRunbookMount,
	})

	req1 := harness.TurnRequest{
		Workdir:   "/tmp/sre",
		Skills:    []json.RawMessage{skillRaw},
		Resources: []json.RawMessage{resLog, resManifest, resRunbook},
	}
	resp1, err := sim.RunTurn(ctx, req1)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp1.PendingCustomToolIDs) != 1 {
		t.Fatalf("turn1 pending=%v want 1", resp1.PendingCustomToolIDs)
	}
	if resp1.PendingCustomToolIDs[0] != SreCustomToolOpenPRID {
		t.Fatalf("turn1 pending id=%q", resp1.PendingCustomToolIDs[0])
	}

	resp2, err := sim.RunTurn(ctx, harness.TurnRequest{Workdir: "/tmp/sre"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp2.PendingCustomToolIDs) != 1 {
		t.Fatalf("turn2 pending=%v want 1", resp2.PendingCustomToolIDs)
	}
	if resp2.PendingCustomToolIDs[0] != SreCustomToolApprovalID {
		t.Fatalf("turn2 pending id=%q", resp2.PendingCustomToolIDs[0])
	}

	resp3, err := sim.RunTurn(ctx, harness.TurnRequest{Workdir: "/tmp/sre"})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp3.PendingCustomToolIDs) != 0 {
		t.Fatalf("turn3 pending=%v want 0", resp3.PendingCustomToolIDs)
	}
	if sim.TurnCount() != 3 {
		t.Fatalf("turns=%d want 3", sim.TurnCount())
	}
}

func TestSreSimulatingClientRejectsMissingResources(t *testing.T) {
	sim := &SreSimulatingClient{}
	skillRaw, _ := json.Marshal(map[string]any{"type": "skill", "name": "x"})
	_, err := sim.RunTurn(context.Background(), harness.TurnRequest{
		Workdir:   "/tmp/sre",
		Skills:    []json.RawMessage{skillRaw},
		Resources: []json.RawMessage{},
	})
	if err == nil {
		t.Fatal("expected error for missing resources")
	}
}
