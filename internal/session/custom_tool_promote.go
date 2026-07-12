package session

import (
	"encoding/json"
)

// appendEventsForPendingPromote returns session_events rows to persist when
// promoting a pending queue item. user.custom_tool_result also synthesizes
// agent.tool_result so the harness history includes the round-trip (AMA parity).
func appendEventsForPendingPromote(payload json.RawMessage) ([]json.RawMessage, error) {
	// Peek at the type first — only user.custom_tool_result needs the full
	// parse. Other pending events (user.message with string/array content,
	// user.tool_confirmation, etc.) must not fail on the Content field.
	var typeOnly struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(payload, &typeOnly); err != nil {
		return nil, err
	}
	if typeOnly.Type != "user.custom_tool_result" {
		return []json.RawMessage{payload}, nil
	}

	var meta struct {
		Type            string            `json:"type"`
		CustomToolUseID string            `json:"custom_tool_use_id"`
		ToolUseID       string            `json:"tool_use_id"`
		Content         []json.RawMessage `json:"content"`
		IsError         *bool             `json:"is_error"`
	}
	if err := json.Unmarshal(payload, &meta); err != nil {
		return nil, err
	}

	toolUseID := meta.CustomToolUseID
	if toolUseID == "" {
		toolUseID = meta.ToolUseID
	}
	if toolUseID == "" {
		return []json.RawMessage{payload}, nil
	}

	toolResult := map[string]any{
		"type":        "agent.tool_result",
		"tool_use_id": toolUseID,
		"content":     meta.Content,
	}
	if meta.IsError != nil {
		toolResult["is_error"] = *meta.IsError
	}
	toolResultJSON, err := json.Marshal(toolResult)
	if err != nil {
		return nil, err
	}
	return []json.RawMessage{payload, toolResultJSON}, nil
}
