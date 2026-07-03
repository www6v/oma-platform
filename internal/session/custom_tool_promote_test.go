package session

import (
	"encoding/json"
	"testing"
)

func TestAppendEventsForPendingPromoteCustomToolResult(t *testing.T) {
	t.Parallel()
	payload := json.RawMessage(`{
		"type":"user.custom_tool_result",
		"custom_tool_use_id":"ctu_1",
		"content":[{"type":"text","text":"{\"action\":\"approve\"}"}]
	}`)
	out, err := appendEventsForPendingPromote(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("events=%d want 2", len(out))
	}
	var userEv struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(out[0], &userEv); err != nil {
		t.Fatal(err)
	}
	if userEv.Type != "user.custom_tool_result" {
		t.Fatalf("first type=%q", userEv.Type)
	}
	var toolEv struct {
		Type      string            `json:"type"`
		ToolUseID string            `json:"tool_use_id"`
		Content   []json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(out[1], &toolEv); err != nil {
		t.Fatal(err)
	}
	if toolEv.Type != "agent.tool_result" {
		t.Fatalf("second type=%q", toolEv.Type)
	}
	if toolEv.ToolUseID != "ctu_1" {
		t.Fatalf("tool_use_id=%q", toolEv.ToolUseID)
	}
	if len(toolEv.Content) != 1 {
		t.Fatalf("content=%v", toolEv.Content)
	}
}

func TestAppendEventsForPendingPromotePassthrough(t *testing.T) {
	t.Parallel()
	payload := json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"hi"}]}`)
	out, err := appendEventsForPendingPromote(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 {
		t.Fatalf("events=%d want 1", len(out))
	}
	if string(out[0]) != string(payload) {
		t.Fatalf("payload mutated: %s", out[0])
	}
}

func TestAppendEventsForPendingPromoteIsError(t *testing.T) {
	t.Parallel()
	payload, err := json.Marshal(map[string]any{
		"type":                "user.custom_tool_result",
		"custom_tool_use_id": "ctu_err",
		"is_error":            true,
		"content": []map[string]string{
			{"type": "text", "text": "rejected by reviewer"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	out, err := appendEventsForPendingPromote(payload)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("events=%d want 2", len(out))
	}
	var toolEv map[string]any
	if err := json.Unmarshal(out[1], &toolEv); err != nil {
		t.Fatal(err)
	}
	if toolEv["is_error"] != true {
		t.Fatalf("is_error=%v want true", toolEv["is_error"])
	}
}
