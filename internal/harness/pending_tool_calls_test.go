package harness

import (
	"encoding/json"
	"testing"
)

func TestBuildPendingToolCallsCustomTools(t *testing.T) {
	t.Parallel()
	events := []json.RawMessage{
		json.RawMessage(`{"type":"agent.custom_tool_use","id":"ctu_a","name":"decide","input":{"receipt_id":"r01"}}`),
		json.RawMessage(`{"type":"agent.custom_tool_use","id":"ctu_b","name":"escalate","input":{"receipt_id":"r02"}}`),
	}
	calls := BuildPendingToolCalls(events, []string{"ctu_a", "ctu_b"})
	if len(calls) != 2 {
		t.Fatalf("calls=%d want 2", len(calls))
	}
	if calls[0].ToolCallID != "ctu_a" || calls[0].ToolName != "decide" {
		t.Fatalf("call[0]=%+v", calls[0])
	}
	if calls[1].Args["receipt_id"] != "r02" {
		t.Fatalf("call[1].args=%v", calls[1].Args)
	}
}

func TestBuildPendingToolCallsBuiltinToolUse(t *testing.T) {
	t.Parallel()
	events := []json.RawMessage{
		json.RawMessage(`{"type":"agent.tool_use","id":"toolu_x","name":"bash","input":{"command":"ls"}}`),
	}
	calls := BuildPendingToolCalls(events, []string{"toolu_x"})
	if len(calls) != 1 || calls[0].ToolName != "bash" {
		t.Fatalf("calls=%+v", calls)
	}
}
