package harness

import (
	"encoding/json"
)

// PendingToolCall mirrors open-managed-agents session metadata so clients
// can render HITL forms without re-walking the full event log.
type PendingToolCall struct {
	ToolCallID string         `json:"tool_call_id"`
	ToolName   string         `json:"tool_name"`
	Args       map[string]any `json:"args,omitempty"`
}

// BuildPendingToolCalls resolves pending tool ids to tool_use payloads.
func BuildPendingToolCalls(
	events []json.RawMessage,
	pendingIDs []string,
) []PendingToolCall {
	if len(pendingIDs) == 0 {
		return nil
	}
	byID := indexToolUses(events)
	out := make([]PendingToolCall, 0, len(pendingIDs))
	for _, id := range pendingIDs {
		use, ok := byID[id]
		if !ok {
			continue
		}
		out = append(out, use)
	}
	return out
}

func indexToolUses(events []json.RawMessage) map[string]PendingToolCall {
	out := make(map[string]PendingToolCall)
	for _, raw := range events {
		var ev struct {
			Type  string         `json:"type"`
			ID    string         `json:"id"`
			Name  string         `json:"name"`
			Input map[string]any `json:"input"`
		}
		if err := json.Unmarshal(raw, &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "agent.custom_tool_use", "agent.tool_use":
			if ev.ID == "" {
				continue
			}
			out[ev.ID] = PendingToolCall{
				ToolCallID: ev.ID,
				ToolName:   ev.Name,
				Args:       ev.Input,
			}
		}
	}
	return out
}
