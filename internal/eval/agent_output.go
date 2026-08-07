package eval

import (
	"encoding/json"
	"strings"
)

// Types that break a contiguous streamed agent.message delta segment.
var agentOutputSegmentBreaks = map[string]struct{}{
	"agent.tool_use":        {},
	"agent.tool_result":     {},
	"agent.custom_tool_use": {},
	"user.message":          {},
}

// AgentOutputFromEvents concatenates assistant text from session events.
// Contiguous streamed agent.message deltas are joined into segments
// (matching harness assemble_assistant_text).
func AgentOutputFromEvents(events []json.RawMessage) string {
	segments := assembleAgentMessageSegments(events)
	if len(segments) == 0 {
		return ""
	}
	return strings.TrimSpace(strings.Join(segments, "\n\n"))
}

// LastAgentOutputFromEvents returns text from the most recent assistant
// segment. Streamed replies are many agent.message deltas; taking only the
// last event would return a trailing fragment (e.g. "year.").
func LastAgentOutputFromEvents(events []json.RawMessage) string {
	segments := assembleAgentMessageSegments(events)
	for i := len(segments) - 1; i >= 0; i-- {
		text := strings.TrimSpace(segments[i])
		if text != "" {
			return text
		}
	}
	return ""
}

func assembleAgentMessageSegments(events []json.RawMessage) []string {
	var segments []string
	var current []string
	flush := func() {
		if len(current) == 0 {
			return
		}
		segments = append(segments, strings.Join(current, ""))
		current = current[:0]
	}
	for _, raw := range events {
		var ev map[string]any
		if err := json.Unmarshal(raw, &ev); err != nil {
			continue
		}
		evType, _ := ev["type"].(string)
		switch {
		case evType == "agent.message":
			text := textFromContent(ev["content"])
			if text != "" {
				current = append(current, text)
			}
		case hasSegmentBreak(evType):
			flush()
		}
	}
	flush()
	return segments
}

func hasSegmentBreak(evType string) bool {
	_, ok := agentOutputSegmentBreaks[evType]
	return ok
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
			if text, ok := m["text"].(string); ok && text != "" {
				// Keep raw piece (including spaces) so delta join is exact.
				parts = append(parts, text)
			}
		}
		return strings.Join(parts, "")
	case string:
		return blocks
	default:
		return ""
	}
}
