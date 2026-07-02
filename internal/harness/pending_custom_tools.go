package harness

import (
	"encoding/json"
	"sort"
	"strings"
)

const (
	// MaxPendingCustomToolEventIDs matches Anthropic managed-agents sliding window.
	MaxPendingCustomToolEventIDs = 5
)

var wireBuiltinToolNames = map[string]struct{}{
	"bash":              {},
	"read":              {},
	"write":             {},
	"edit":              {},
	"glob":              {},
	"grep":              {},
	"web_fetch":         {},
	"web_search":        {},
	"schedule":          {},
	"cancel_schedule":   {},
	"list_schedules":    {},
	"browser":           {},
	"ls":                {},
	"find":              {},
	"team_create":       {},
	"spawn_teammate":    {},
	"send_team_message": {},
	"read_team_messages": {},
}

// IsWireBuiltinTool reports whether a tool name maps to agent.tool_use.
func IsWireBuiltinTool(name string) bool {
	if name == "" {
		return false
	}
	if _, ok := wireBuiltinToolNames[name]; ok {
		return true
	}
	if strings.HasPrefix(name, "mcp_") || strings.HasPrefix(name, "mcp__") {
		return true
	}
	if strings.HasPrefix(name, "call_agent_") {
		return true
	}
	return false
}

// PendingCustomToolIDs returns custom tool use IDs without tool_result.
func PendingCustomToolIDs(events []json.RawMessage) []string {
	type pending struct {
		id   string
		seen bool
	}
	order := make([]pending, 0)
	answered := make(map[string]struct{})

	for _, raw := range events {
		var ev struct {
			Type      string `json:"type"`
			ID        string `json:"id"`
			Name      string `json:"name"`
			ToolUseID string `json:"tool_use_id"`
		}
		if err := json.Unmarshal(raw, &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "agent.custom_tool_use":
			if ev.ID != "" {
				order = append(order, pending{id: ev.ID})
			}
		case "agent.tool_use":
			if ev.ID != "" && !IsWireBuiltinTool(ev.Name) {
				order = append(order, pending{id: ev.ID})
			}
		case "agent.tool_result":
			if ev.ToolUseID != "" {
				answered[ev.ToolUseID] = struct{}{}
			}
		}
	}

	out := make([]string, 0)
	for _, item := range order {
		if _, ok := answered[item.id]; ok {
			continue
		}
		out = append(out, item.id)
	}
	if len(out) > MaxPendingCustomToolEventIDs {
		return out[:MaxPendingCustomToolEventIDs]
	}
	return out
}

// BuildIdleStopReason returns session.status_idle stop_reason payload.
func BuildIdleStopReason(pendingCustom []string) map[string]any {
	if len(pendingCustom) == 0 {
		return map[string]any{"type": "end_turn"}
	}
	ids := append([]string(nil), pendingCustom...)
	sort.Strings(ids)
	if len(ids) > MaxPendingCustomToolEventIDs {
		ids = ids[:MaxPendingCustomToolEventIDs]
	}
	return map[string]any{
		"type":        "requires_action",
		"action_type": "custom_tool_result",
		"event_ids":   ids,
	}
}
