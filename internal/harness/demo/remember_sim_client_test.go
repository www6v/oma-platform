package demo

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/open-ma/oma-building/internal/harness"
)

func TestRememberSimulatingClientSaveAndRecall(t *testing.T) {
	t.Parallel()
	sim := &RememberSimulatingClient{}
	workdir := t.TempDir()

	saveReq := harness.TurnRequest{
		Workdir: workdir,
		Resources: []json.RawMessage{
			json.RawMessage(`{
				"type":"memory_store",
				"store_id":"mst_test",
				"store_name":"user-preferences",
				"read_only":false,
				"memories":[]
			}`),
		},
	}
	err := sim.RunTurnStream(context.Background(), saveReq, func(ev json.RawMessage) error {
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	bindings := harness.MemoryStoreBindings(saveReq.Resources)
	if len(bindings) != 1 {
		t.Fatalf("bindings=%v want 1", bindings)
	}

	recallReq := harness.TurnRequest{
		Workdir: t.TempDir(),
		Resources: []json.RawMessage{
			json.RawMessage(`{
				"type":"memory_store",
				"store_id":"mst_test",
				"store_name":"user-preferences",
				"read_only":false,
				"memories":[{
					"path":"/preferences/formatting.md",
					"content":"User prefers bullet points and concise replies."
				}]
			}`),
		},
	}
	var recallText string
	err = sim.RunTurnStream(context.Background(), recallReq, func(ev json.RawMessage) error {
		var msg struct {
			Type    string `json:"type"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		}
		if json.Unmarshal(ev, &msg) == nil && msg.Type == "agent.message" {
			for _, block := range msg.Content {
				recallText = block.Text
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if recallText == "" || !strings.HasPrefix(recallText, RememberRecallMarker) {
		t.Fatalf("recall=%q want prefix %q", recallText, RememberRecallMarker)
	}
}
