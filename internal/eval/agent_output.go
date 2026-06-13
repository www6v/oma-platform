package eval

import (
	"encoding/json"
	"strings"
)

// AgentOutputFromEvents concatenates assistant text from session events.
func AgentOutputFromEvents(events []json.RawMessage) string {
	var parts []string
	for _, raw := range events {
		var ev map[string]any
		if err := json.Unmarshal(raw, &ev); err != nil {
			continue
		}
		evType, _ := ev["type"].(string)
		if evType != "agent.message" {
			continue
		}
		text := textFromContent(ev["content"])
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func textFromContent(raw any) string {
	switch blocks := raw.(type) {
	case []any:
		var parts []string
		for _, block := range blocks {
			m, ok := block.(map[string]any)
			if !ok {
				continue
			}
			if m["type"] != "text" {
				continue
			}
			if text, ok := m["text"].(string); ok && strings.TrimSpace(text) != "" {
				parts = append(parts, strings.TrimSpace(text))
			}
		}
		return strings.Join(parts, "\n")
	case string:
		return strings.TrimSpace(blocks)
	default:
		return ""
	}
}
