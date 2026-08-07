package eval_test

import (
	"encoding/json"
	"testing"

	"github.com/open-ma/oma-building/internal/eval"
)

func TestAgentOutputFromEvents(t *testing.T) {
	first, _ := json.Marshal(map[string]any{
		"type": "agent.message",
		"content": []map[string]string{
			{"type": "text", "text": "first reply"},
		},
	})
	second, _ := json.Marshal(map[string]any{
		"type": "agent.message",
		"content": []map[string]string{
			{"type": "text", "text": "second reply"},
		},
	})
	out := eval.AgentOutputFromEvents([]json.RawMessage{first, second})
	// Contiguous deltas join into one segment.
	if out != "first replysecond reply" {
		t.Fatalf("output=%q", out)
	}
}

func TestLastAgentOutputFromEvents(t *testing.T) {
	first, _ := json.Marshal(map[string]any{
		"type": "agent.message",
		"content": []map[string]string{
			{"type": "text", "text": "first reply"},
		},
	})
	tool, _ := json.Marshal(map[string]any{
		"type": "agent.tool_use",
		"name": "bash",
	})
	second, _ := json.Marshal(map[string]any{
		"type": "agent.message",
		"content": []map[string]string{
			{"type": "text", "text": "second reply"},
		},
	})
	out := eval.LastAgentOutputFromEvents(
		[]json.RawMessage{first, tool, second},
	)
	if out != "second reply" {
		t.Fatalf("output=%q want second reply only", out)
	}
}

func TestLastAgentOutputFromEventsJoinsStreamDeltas(t *testing.T) {
	chunks := []string{
		"Q1 revenue reached ",
		"$4.2M, up 12% ",
		"from Q4.",
	}
	events := make([]json.RawMessage, 0, len(chunks))
	for _, chunk := range chunks {
		raw, _ := json.Marshal(map[string]any{
			"type": "agent.message",
			"content": []map[string]string{
				{"type": "text", "text": chunk},
			},
		})
		events = append(events, raw)
	}
	out := eval.LastAgentOutputFromEvents(events)
	want := "Q1 revenue reached $4.2M, up 12% from Q4."
	if out != want {
		t.Fatalf("output=%q want %q", out, want)
	}
}
