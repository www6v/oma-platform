package linear

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RenderUserMessage builds the user.message text for a normalized webhook.
func RenderUserMessage(event *NormalizedWebhookEvent) string {
	actor := "(unknown)"
	if event.ActorUserName != "" {
		actor = "@" + event.ActorUserName
	}
	header := fmt.Sprintf("Linear %s", event.Kind)
	if event.Kind == KindAgentSessionPrompted {
		header = "Linear agent session — new prompt"
	}
	if event.Kind == KindAgentSessionCreated {
		header = "Linear agent session — newly opened"
	}
	lines := []string{
		"# " + header,
		"",
		fmt.Sprintf("**Issue:** %s", fallback(event.IssueIdentifier, "?")),
	}
	if event.IssueID != "" {
		lines = append(lines,
			fmt.Sprintf("**Issue UUID:** `%s`", event.IssueID),
		)
	}
	lines = append(lines, fmt.Sprintf("**Actor:** %s", actor))
	if event.AgentSessionID != "" {
		lines = append(lines,
			fmt.Sprintf("**Linear panel:** `%s`", event.AgentSessionID),
		)
	}
	if event.IssueTitle != "" {
		lines = append(lines, "", "**Title:** "+event.IssueTitle)
	}
	if event.IssueDescription != "" {
		lines = append(lines, "", "**Description:**", event.IssueDescription)
	}
	if event.CommentBody != "" {
		lines = append(lines, "", "**Source comment:**",
			"> "+strings.ReplaceAll(event.CommentBody, "\n", "\n> "),
		)
	}
	return strings.Join(lines, "\n")
}

// BuildUserMessageEvent returns a user.message JSON payload.
func BuildUserMessageEvent(
	event *NormalizedWebhookEvent,
	publicationID string,
) (json.RawMessage, error) {
	body := map[string]any{
		"type": "user.message",
		"content": []map[string]string{
			{"type": "text", "text": RenderUserMessage(event)},
		},
		"metadata": map[string]any{
			"linear": map[string]string{
				"publicationId": publicationID,
			},
		},
	}
	return json.Marshal(body)
}

func fallback(value, def string) string {
	if value == "" {
		return def
	}
	return value
}
