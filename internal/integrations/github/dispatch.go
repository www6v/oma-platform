package github

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RenderUserMessage builds user.message text for a normalized event.
func RenderUserMessage(event *NormalizedEvent) string {
	header := "GitHub " + event.Kind
	if event.IssueTitle != "" {
		header = fmt.Sprintf("GitHub %s — %s", event.Kind, event.IssueTitle)
	}
	lines := []string{
		"# " + header,
		"",
	}
	if event.IssueKey != "" {
		lines = append(lines, fmt.Sprintf("**Issue:** %s", event.IssueKey))
	}
	if event.ActorLogin != "" {
		lines = append(lines, fmt.Sprintf("**Actor:** @%s", event.ActorLogin))
	}
	if event.HTMLURL != "" {
		lines = append(lines, fmt.Sprintf("**URL:** %s", event.HTMLURL))
	}
	if event.IssueBody != "" {
		lines = append(lines, "", "**Description:**", event.IssueBody)
	}
	if event.CommentBody != "" {
		lines = append(lines, "", "**Comment:**",
			"> "+strings.ReplaceAll(event.CommentBody, "\n", "\n> "),
		)
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
			"github": map[string]string{
				"publicationId": publicationID,
				"issueKey":      event.IssueKey,
			},
		},
	}
	return json.Marshal(body)
}
