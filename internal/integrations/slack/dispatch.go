package slack

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RenderUserMessage builds user.message text for a normalized event.
func RenderUserMessage(event *NormalizedEvent) string {
	lines := []string{
		"# Slack app mention",
		"",
	}
	if event.ChannelID != "" {
		lines = append(lines, fmt.Sprintf("**Channel:** `%s`", event.ChannelID))
	}
	if event.ThreadTS != "" {
		lines = append(lines, fmt.Sprintf("**Thread:** `%s`", event.ThreadTS))
	}
	if event.UserID != "" {
		lines = append(lines, fmt.Sprintf("**User:** <@%s>", event.UserID))
	}
	if event.Text != "" {
		lines = append(lines, "", event.Text)
	}
	return strings.Join(lines, "\n")
}

// BuildUserMessageEvent returns a user.message JSON payload.
func BuildUserMessageEvent(
	event *NormalizedEvent,
	publicationID string,
) (json.RawMessage, error) {
	body := map[string]any{
		"type": "user.message",
		"content": []map[string]string{
			{"type": "text", "text": RenderUserMessage(event)},
		},
		"metadata": map[string]any{
			"slack": map[string]string{
				"publicationId": publicationID,
				"scopeKey":        event.ScopeKey,
			},
		},
	}
	return json.Marshal(body)
}
