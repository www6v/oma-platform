package demo

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/open-ma/oma-building/internal/harness"
)

func TestCoordinateSimulatingClientSubAgents(t *testing.T) {
	t.Parallel()
	sim := &CoordinateSimulatingClient{}
	req := harness.TurnRequest{
		Workdir: "/tmp/wd",
		Resources: []json.RawMessage{
			json.RawMessage(`{"type":"file","mount_path":"/mnt/user-data/x.md"}`),
		},
		SubAgents: map[string]harness.AgentSnapshot{
			"agt_r": {ID: "agt_r", Name: SpecialistResearcher},
			"agt_l": {ID: "agt_l", Name: SpecialistLibrarian},
			"agt_p": {ID: "agt_p", Name: SpecialistPricer},
		},
	}
	var count int
	err := sim.RunTurnStream(context.Background(), req, func(ev json.RawMessage) error {
		count++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count < 10 {
		t.Fatalf("events=%d want >=10", count)
	}
	names := sim.SubAgentNames()
	if len(names) != 3 {
		t.Fatalf("subagent names=%v want 3", names)
	}
}
