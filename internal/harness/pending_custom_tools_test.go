package harness

import (
	"encoding/json"
	"testing"
)

func TestPendingCustomToolIDs(t *testing.T) {
	t.Parallel()
	events := []json.RawMessage{
		json.RawMessage(`{"type":"agent.custom_tool_use","id":"ctu_a","name":"decide"}`),
		json.RawMessage(`{"type":"agent.custom_tool_use","id":"ctu_b","name":"escalate"}`),
		json.RawMessage(`{"type":"agent.tool_result","tool_use_id":"ctu_a","content":[]}`),
	}
	pending := PendingCustomToolIDs(events)
	if len(pending) != 1 || pending[0] != "ctu_b" {
		t.Fatalf("pending=%v want [ctu_b]", pending)
	}
}

func TestBuildIdleStopReasonRequiresAction(t *testing.T) {
	t.Parallel()
	reason := BuildIdleStopReason([]string{"ctu_1", "ctu_2"})
	if reason["type"] != "requires_action" {
		t.Fatalf("type=%v", reason["type"])
	}
	if reason["action_type"] != "custom_tool_result" {
		t.Fatalf("action_type=%v", reason["action_type"])
	}
	ids, ok := reason["event_ids"].([]string)
	if !ok || len(ids) != 2 {
		t.Fatalf("event_ids=%v", reason["event_ids"])
	}
}

func TestBuildIdleStopReasonEndTurn(t *testing.T) {
	t.Parallel()
	reason := BuildIdleStopReason(nil)
	if reason["type"] != "end_turn" {
		t.Fatalf("type=%v", reason["type"])
	}
}

func TestBuildIdleStopReasonSlidingWindow(t *testing.T) {
	t.Parallel()
	pending := []string{
		"ctu_0", "ctu_1", "ctu_2", "ctu_3", "ctu_4", "ctu_5",
	}
	reason := BuildIdleStopReason(pending)
	if reason["type"] != "requires_action" {
		t.Fatalf("type=%v", reason["type"])
	}
	ids, ok := reason["event_ids"].([]string)
	if !ok || len(ids) != MaxPendingCustomToolEventIDs {
		t.Fatalf("event_ids=%v want len %d", reason["event_ids"], MaxPendingCustomToolEventIDs)
	}
	want := []string{"ctu_0", "ctu_1", "ctu_2", "ctu_3", "ctu_4"}
	for idx, id := range want {
		if ids[idx] != id {
			t.Fatalf("event_ids[%d]=%q want %q (full=%v)", idx, ids[idx], id, ids)
		}
	}
}

func TestIsWireBuiltinTool(t *testing.T) {
	t.Parallel()
	if !IsWireBuiltinTool("bash") {
		t.Fatal("bash should be builtin")
	}
	if IsWireBuiltinTool("decide") {
		t.Fatal("decide should not be builtin")
	}
}
