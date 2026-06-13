package harness_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/open-ma/oma-building/internal/harness"
)

func TestSubAgentSimulatingClientValidatesSubAgents(t *testing.T) {
	client := &harness.SubAgentSimulatingClient{}
	callable, _ := json.Marshal([]map[string]any{
		{"type": "agent", "id": "agt_worker", "version": 1},
	})

	err := client.RunTurnStream(
		context.Background(),
		harness.TurnRequest{
			SessionID: "sess_x",
			Agent: harness.AgentSnapshot{
				ID:             "agt_coord",
				CallableAgents: callable,
			},
			Events: []json.RawMessage{
				json.RawMessage(`{"type":"user.message"}`),
			},
			Workdir: "/tmp",
		},
		func(json.RawMessage) error { return nil },
	)
	if err == nil {
		t.Fatal("expected error when sub_agents missing")
	}
}

func TestSubAgentSimulatingClientEmitsDelegationEvents(t *testing.T) {
	client := &harness.SubAgentSimulatingClient{
		WorkerReply:  "worker-reply",
		PrimaryReply: "primary-reply",
	}
	callable, _ := json.Marshal([]map[string]any{
		{"type": "agent", "id": "agt_worker", "version": 1},
	})

	var got []map[string]any
	err := client.RunTurnStream(
		context.Background(),
		harness.TurnRequest{
			SessionID: "sess_x",
			Agent: harness.AgentSnapshot{
				ID:             "agt_coord",
				CallableAgents: callable,
			},
			SubAgents: map[string]harness.AgentSnapshot{
				"agt_worker": {ID: "agt_worker", Name: "Worker"},
			},
			Workdir: "/tmp",
		},
		func(raw json.RawMessage) error {
			var item map[string]any
			if err := json.Unmarshal(raw, &item); err != nil {
				return err
			}
			got = append(got, item)
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	types := make([]string, 0, len(got))
	for _, ev := range got {
		types = append(types, ev["type"].(string))
	}
	want := []string{
		"session.thread_created",
		"agent.message",
		"session.thread_idle",
		"agent.tool_use",
		"agent.message",
	}
	if len(types) != len(want) {
		t.Fatalf("events=%v", types)
	}
	for i, typ := range want {
		if types[i] != typ {
			t.Fatalf("event[%d]=%q want %q all=%v", i, types[i], typ, types)
		}
	}
	if got[0]["session_thread_id"] != "sthr_e2e_worker" {
		t.Fatalf("thread_id=%v", got[0]["session_thread_id"])
	}
}
