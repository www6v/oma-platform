package github

import (
	"encoding/json"
	"fmt"
	"strings"
)

const (
	KindIssueEngaged = "issue_engaged"
	KindIssueComment = "issue_comment"
)

// NormalizedEvent is a webhook narrowed to dispatch-relevant fields.
type NormalizedEvent struct {
	Kind          string
	EventType     string
	Action        string
	DeliveryID    string
	Repository    string
	ItemNumber    int
	IssueKey      string
	IssueTitle    string
	IssueBody     string
	CommentBody   string
	ActorLogin    string
	HTMLURL       string
	TriggerLabel  string
	HasTrigger    bool
}

type rawEnvelope struct {
	Action     string       `json:"action"`
	Repository *rawRepo     `json:"repository"`
	Issue      *rawIssue    `json:"issue"`
	Label      *rawLabel    `json:"label"`
	Comment    *rawComment  `json:"comment"`
	Sender     *rawUser     `json:"sender"`
}

type rawRepo struct {
	FullName string `json:"full_name"`
}

type rawIssue struct {
	Number  int        `json:"number"`
	Title   string     `json:"title"`
	Body    *string    `json:"body"`
	HTMLURL string     `json:"html_url"`
	Labels  []rawLabel `json:"labels"`
}

type rawLabel struct {
	Name string `json:"name"`
}

type rawComment struct {
	Body string `json:"body"`
}

type rawUser struct {
	Login string `json:"login"`
}

// ParseWebhook parses a GitHub delivery for MVP routing.
func ParseWebhook(
	eventType, action string,
	rawBody []byte,
	triggerLabel string,
) (*NormalizedEvent, error) {
	var env rawEnvelope
	if err := json.Unmarshal(rawBody, &env); err != nil {
		return nil, err
	}
	if action == "" {
		action = env.Action
	}
	event := &NormalizedEvent{
		EventType:    eventType,
		Action:       action,
		TriggerLabel: triggerLabel,
	}
	if env.Repository != nil {
		event.Repository = env.Repository.FullName
	}
	if env.Sender != nil {
		event.ActorLogin = env.Sender.Login
	}
	if env.Issue != nil {
		event.ItemNumber = env.Issue.Number
		if event.Repository != "" && env.Issue.Number > 0 {
			event.IssueKey = fmt.Sprintf("%s#%d", event.Repository, env.Issue.Number)
		}
		event.IssueTitle = env.Issue.Title
		if env.Issue.Body != nil {
			event.IssueBody = *env.Issue.Body
		}
		event.HTMLURL = env.Issue.HTMLURL
		event.HasTrigger = issueHasLabel(env.Issue, triggerLabel)
	}
	if env.Comment != nil {
		event.CommentBody = env.Comment.Body
	}

	switch eventType {
	case "issues":
		if action == "labeled" && labelMatches(env.Label, triggerLabel) {
			event.Kind = KindIssueEngaged
			return event, nil
		}
	case "issue_comment":
		if action == "created" && event.HasTrigger {
			event.Kind = KindIssueComment
			return event, nil
		}
	}
	return event, nil
}

func labelMatches(label *rawLabel, trigger string) bool {
	if label == nil || trigger == "" {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(label.Name), trigger)
}

func issueHasLabel(issue *rawIssue, trigger string) bool {
	if issue == nil || trigger == "" {
		return false
	}
	for _, label := range issue.Labels {
		if strings.EqualFold(strings.TrimSpace(label.Name), trigger) {
			return true
		}
	}
	return false
}
