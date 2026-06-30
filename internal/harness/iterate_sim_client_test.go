package harness_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/open-ma/oma-building/internal/harness"
)

func TestIterateSimulatingClientTwoTurns(t *testing.T) {
	client := &harness.IterateSimulatingClient{}
	workdir := t.TempDir()

	buggyCalc := "def add(a, b):\n    return a + b + 1  # BUG\n"
	testCalc := "from calc import add\n"

	resources := []json.RawMessage{
		mustJSON(map[string]any{
			"type":           "file",
			"mount_path":     "calc.py",
			"content_base64": base64.StdEncoding.EncodeToString([]byte(buggyCalc)),
		}),
		mustJSON(map[string]any{
			"type":           "file",
			"mount_path":     "test_calc.py",
			"content_base64": base64.StdEncoding.EncodeToString([]byte(testCalc)),
		}),
	}

	runTurn := func() string {
		var lastText string
		req := harness.TurnRequest{
			SessionID: "sess_1",
			Workdir:   workdir,
			Resources: resources,
		}
		err := client.RunTurnStream(context.Background(), req, func(raw json.RawMessage) error {
			var ev map[string]any
			if json.Unmarshal(raw, &ev) != nil {
				return nil
			}
			if ev["type"] != "agent.message" {
				return nil
			}
			content, _ := ev["content"].([]any)
			for _, block := range content {
				m, _ := block.(map[string]any)
				if m["type"] == "text" {
					lastText, _ = m["text"].(string)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("turn: %v", err)
		}
		return lastText
	}

	turn1 := runTurn()
	if !strings.Contains(turn1, harness.IterateTurn1Marker) {
		t.Fatalf("turn 1 marker missing: %q", turn1)
	}

	turn2 := runTurn()
	if !strings.Contains(turn2, harness.IterateTurn2Marker) {
		t.Fatalf("turn 2 marker missing: %q", turn2)
	}

	if client.TurnCount() != 2 {
		t.Fatalf("turn count=%d want 2", client.TurnCount())
	}
}
