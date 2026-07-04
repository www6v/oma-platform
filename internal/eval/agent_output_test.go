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
	if out != "first reply\n\nsecond reply" {
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
	second, _ := json.Marshal(map[string]any{
		"type": "agent.message",
		"content": []map[string]string{
			{"type": "text", "text": "second reply"},
		},
	})
	out := eval.LastAgentOutputFromEvents([]json.RawMessage{first, second})
	if out != "second reply" {
		t.Fatalf("output=%q want second reply only", out)
	}
}
