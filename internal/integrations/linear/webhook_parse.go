package linear

import (
	"encoding/json"
	"strings"
)

// NotificationKind is a routable Linear webhook subtype.
type NotificationKind string

const (
	KindIssueAssignedToYou   NotificationKind = "issueAssignedToYou"
	KindIssueMention         NotificationKind = "issueMention"
	KindIssueCommentMention  NotificationKind = "issueCommentMention"
	KindIssueNewComment      NotificationKind = "issueNewComment"
	KindAgentSessionCreated  NotificationKind = "agentSessionCreated"
	KindAgentSessionPrompted NotificationKind = "agentSessionPrompted"
	KindCommentReply         NotificationKind = "commentReply"
)

// NormalizedWebhookEvent is the parsed webhook shape used for dispatch.
type NormalizedWebhookEvent struct {
	Kind            NotificationKind
	WorkspaceID     string
	IssueID         string
	IssueIdentifier string
	IssueTitle      string
	IssueDescription string
	CommentBody     string
	CommentID       string
	ActorUserID     string
	ActorUserName   string
	DeliveryID      string
	EventType       string
	AgentSessionID  string
	PromptContext   string
}

type rawWebhookEnvelope struct {
	Type             string          `json:"type"`
	Action           string          `json:"action"`
	WebhookID        string          `json:"webhookId"`
	OrganizationID   string          `json:"organizationId"`
	Data             json.RawMessage `json:"data"`
	Notification     json.RawMessage `json:"notification"`
	AgentSession     json.RawMessage `json:"agentSession"`
	PromptContext    string          `json:"promptContext"`
}

// ParseWebhook narrows a Linear webhook body to a routable event.
func ParseWebhook(rawBody []byte) (*NormalizedWebhookEvent, error) {
	var raw rawWebhookEnvelope
	if err := json.Unmarshal(rawBody, &raw); err != nil {
		return nil, err
	}
	deliveryID := raw.WebhookID
	if deliveryID == "" {
		return nil, nil
	}
	eventType := raw.Type
	action := raw.Action

	switch eventType {
	case "AppUserNotification":
		return parseAppUserNotification(raw, deliveryID, eventType, action)
	case "AgentSessionEvent":
		return parseAgentSessionEvent(raw, deliveryID, eventType, action)
	case "Comment":
		if action == "create" {
			return parseCommentCreate(raw, deliveryID, eventType)
		}
	}
	return &NormalizedWebhookEvent{
		Kind:       "",
		WorkspaceID: raw.OrganizationID,
		DeliveryID: deliveryID,
		EventType:  eventType,
	}, nil
}

func parseAppUserNotification(
	raw rawWebhookEnvelope,
	deliveryID, eventType, action string,
) (*NormalizedWebhookEvent, error) {
	src := raw.Notification
	if len(src) == 0 {
		src = raw.Data
	}
	var notif map[string]any
	if err := json.Unmarshal(src, &notif); err != nil {
		return nil, err
	}
	subtype := pickString(notif, "type")
	if subtype == "" {
		subtype = action
	}
	kind := mapNotificationKind(subtype)
	issue := pickObject(notif, "issue")
	comment := pickObject(notif, "comment")
	actor := pickObject(notif, "actor")
	return &NormalizedWebhookEvent{
		Kind:            kind,
		WorkspaceID:     firstNonEmpty(raw.OrganizationID, pickString(issue, "organizationId")),
		IssueID:         pickString(issue, "id"),
		IssueIdentifier: pickString(issue, "identifier"),
		IssueTitle:      pickString(issue, "title"),
		IssueDescription: pickString(issue, "description"),
		CommentBody:     pickString(comment, "body"),
		CommentID:       pickString(comment, "id"),
		ActorUserID:     pickString(actor, "id"),
		ActorUserName:   pickString(actor, "name"),
		DeliveryID:      deliveryID,
		EventType:       eventType,
	}, nil
}

func parseAgentSessionEvent(
	raw rawWebhookEnvelope,
	deliveryID, eventType, action string,
) (*NormalizedWebhookEvent, error) {
	var session map[string]any
	if len(raw.AgentSession) > 0 {
		_ = json.Unmarshal(raw.AgentSession, &session)
	}
	if session == nil && len(raw.Data) > 0 {
		var data map[string]any
		_ = json.Unmarshal(raw.Data, &data)
		session = pickObject(data, "agentSession")
	}
	issue := pickObject(session, "issue")
	creator := pickObject(session, "creator")
	comment := pickObject(session, "comment")
	var kind NotificationKind
	switch action {
	case "created":
		kind = KindAgentSessionCreated
	case "prompted":
		kind = KindAgentSessionPrompted
	}
	return &NormalizedWebhookEvent{
		Kind:            kind,
		WorkspaceID:     firstNonEmpty(raw.OrganizationID, pickString(session, "organizationId")),
		IssueID:         firstNonEmpty(pickString(issue, "id"), pickString(session, "issueId")),
		IssueIdentifier: pickString(issue, "identifier"),
		IssueTitle:      pickString(issue, "title"),
		IssueDescription: pickString(issue, "description"),
		CommentBody:     pickString(comment, "body"),
		CommentID:       firstNonEmpty(pickString(comment, "id"), pickString(session, "commentId")),
		ActorUserID:     pickString(creator, "id"),
		ActorUserName:   pickString(creator, "name"),
		DeliveryID:      deliveryID,
		EventType:       eventType,
		AgentSessionID:  pickString(session, "id"),
		PromptContext:   decodeHTMLEntities(raw.PromptContext),
	}, nil
}

func parseCommentCreate(
	raw rawWebhookEnvelope,
	deliveryID, eventType string,
) (*NormalizedWebhookEvent, error) {
	var data map[string]any
	if err := json.Unmarshal(raw.Data, &data); err != nil {
		return nil, err
	}
	parent := pickObject(data, "parent")
	parentID := pickString(data, "parentId")
	if parentID == "" && parent != nil {
		parentID = pickString(parent, "id")
	}
	issueID := pickString(data, "issueId")
	var kind NotificationKind
	if issueID != "" {
		kind = KindCommentReply
	}
	return &NormalizedWebhookEvent{
		Kind:        kind,
		WorkspaceID: raw.OrganizationID,
		IssueID:     issueID,
		CommentBody: pickString(data, "body"),
		CommentID:   pickString(data, "id"),
		ActorUserID: pickString(data, "userId"),
		DeliveryID:  deliveryID,
		EventType:   eventType,
	}, nil
}

func mapNotificationKind(subtype string) NotificationKind {
	switch subtype {
	case string(KindIssueAssignedToYou):
		return KindIssueAssignedToYou
	case string(KindIssueMention):
		return KindIssueMention
	case string(KindIssueCommentMention):
		return KindIssueCommentMention
	case string(KindIssueNewComment):
		return KindIssueNewComment
	default:
		return ""
	}
}

func pickObject(o map[string]any, key string) map[string]any {
	if o == nil {
		return nil
	}
	v, ok := o[key].(map[string]any)
	if !ok {
		return nil
	}
	return v
}

func pickString(o map[string]any, key string) string {
	if o == nil {
		return ""
	}
	v, ok := o[key].(string)
	if !ok {
		return ""
	}
	return v
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func decodeHTMLEntities(s string) string {
	if s == "" {
		return ""
	}
	r := strings.NewReplacer(
		"&lt;", "<",
		"&gt;", ">",
		"&quot;", "\"",
		"&#39;", "'",
		"&amp;", "&",
	)
	return r.Replace(s)
}
