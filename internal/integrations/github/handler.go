package github

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/open-ma/oma-building/internal/store"
)

// WebhookOutcome describes how a webhook delivery was handled.
type WebhookOutcome struct {
	Handled       bool
	Reason        string
	SessionID     string
	PublicationID string
	TenantID      string
}

// SessionDispatcher creates or resumes sessions for integration events.
type SessionDispatcher interface {
	DispatchUserMessage(
		ctx context.Context,
		input DispatchInput,
	) (sessionID string, err error)
	ResumeUserMessage(
		ctx context.Context,
		sessionID string,
		userEvent []byte,
	) error
}

// DispatchInput is the publication context for a new session.
type DispatchInput struct {
	TenantID      string
	UserID        string
	AgentID       string
	EnvironmentID string
	Title         string
	UserEvent     []byte
	PublicationID string
	IssueKey      string
	PerIssue      bool
}

// Handler orchestrates GitHub webhook dispatch.
type Handler struct {
	Integrations *store.IntegrationRepo
	Origin       string
	Dispatch     SessionDispatcher
}

// PublicationWebhookURI builds the webhook URL surfaced to GitHub admins.
func PublicationWebhookURI(origin, pubID string) string {
	return strings.TrimRight(origin, "/") +
		"/github/webhook/pub/" + pubID
}

// BindMockInstallation completes a publication without calling GitHub APIs.
func (h *Handler) BindMockInstallation(
	ctx context.Context,
	pubID, workspaceID, workspaceName, botUserID string,
) error {
	pub, err := h.Integrations.GetPublication(ctx, store.ProviderGitHub, pubID)
	if err != nil {
		return err
	}
	if pub == nil {
		return fmt.Errorf("unknown publication")
	}
	if pub.Status == "live" {
		return nil
	}
	instID := "gh_inst_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	inst, err := h.Integrations.InsertProviderInstallation(ctx, store.NewProviderInstallation{
		ID:            instID,
		TenantID:      pub.TenantID,
		UserID:        pub.UserID,
		Provider:      store.ProviderGitHub,
		WorkspaceID:   workspaceID,
		WorkspaceName: workspaceName,
		BotUserID:     botUserID,
	})
	if err != nil {
		return err
	}
	return h.Integrations.BindProviderPublication(
		ctx, store.ProviderGitHub, pubID, store.BindProviderPublication{
			InstallationID: inst.ID,
		},
	)
}

// HandleWebhook verifies, parses, and dispatches a GitHub webhook delivery.
func (h *Handler) HandleWebhook(
	ctx context.Context,
	pubID, deliveryID string,
	headers map[string]string,
	rawBody []byte,
) (WebhookOutcome, error) {
	out := WebhookOutcome{Reason: "unknown"}
	if pubID == "" {
		out.Reason = "missing_publication_id"
		return out, nil
	}
	if deliveryID == "" {
		out.Reason = "missing_delivery_id"
		return out, nil
	}

	pub, err := h.Integrations.GetPublication(ctx, store.ProviderGitHub, pubID)
	if err != nil {
		return out, err
	}
	if pub == nil {
		out.Reason = "publication_not_found"
		return out, nil
	}
	out.PublicationID = pub.ID
	out.TenantID = pub.TenantID
	if pub.Status == "unpublished" {
		out.Reason = "publication_unpublished"
		return out, nil
	}
	if pub.InstallationID == "" {
		out.Reason = "publication_not_live"
		return out, nil
	}
	inst, err := h.Integrations.GetInstallation(
		ctx, store.ProviderGitHub, pub.InstallationID,
	)
	if err != nil {
		return out, err
	}
	if inst == nil || inst.RevokedAt != nil {
		out.Reason = "installation_not_found_or_revoked"
		return out, nil
	}

	secret, err := h.Integrations.GetPublicationWebhookSecret(
		ctx, store.ProviderGitHub, pubID,
	)
	if err != nil {
		return out, err
	}
	if secret == "" {
		out.Reason = "missing_webhook_secret"
		return out, nil
	}
	signature := headers["x-hub-signature-256"]
	if signature == "" {
		out.Reason = "missing_signature"
		return out, nil
	}
	if !VerifyWebhookSignature(secret, rawBody, signature) {
		out.Reason = "invalid_signature"
		return out, nil
	}

	fresh, err := h.Integrations.RecordWebhookDeliveryIfNew(
		ctx, deliveryID, "github", pubID, inst.ID,
	)
	if err != nil {
		return out, err
	}
	if !fresh {
		out.Reason = "duplicate_delivery"
		return out, nil
	}

	eventType := headers["x-github-event"]
	triggerLabel := store.PublicationTriggerLabel(pub.PersonaName)
	event, err := ParseWebhook(eventType, "", rawBody, triggerLabel)
	if err != nil {
		out.Reason = "invalid_json"
		return out, nil
	}
	if event == nil || event.Kind == "" {
		out.Reason = "ignored_event_" + eventType
		return out, nil
	}
	if event.ActorLogin != "" && inst.BotUserID != "" &&
		strings.EqualFold(event.ActorLogin, inst.BotUserID) {
		out.Reason = "event_from_bot_self"
		return out, nil
	}

	userEvent, err := BuildUserMessageEvent(event, pub.ID)
	if err != nil {
		return out, err
	}
	if h.Dispatch == nil {
		out.Reason = "dispatch_not_configured"
		return out, nil
	}

	envID := ""
	if pub.EnvironmentID != nil {
		envID = *pub.EnvironmentID
	}
	title := event.IssueTitle
	if title == "" {
		title = "GitHub " + event.Kind
	}

	perIssue := pub.SessionGranularity == "per_issue" && event.IssueKey != ""
	if event.Kind == KindIssueComment && perIssue {
		existing, err := h.Integrations.GetGitHubIssueSession(
			ctx, pub.ID, event.IssueKey,
		)
		if err != nil {
			return out, err
		}
		if existing == nil || existing.Status != "active" {
			out.Reason = "comment_on_issue_with_no_active_session"
			return out, nil
		}
		if err := h.Dispatch.ResumeUserMessage(
			ctx, existing.SessionID, userEvent,
		); err != nil {
			out.Reason = "comment_resume_failed"
			return out, err
		}
		out.Handled = true
		out.Reason = "comment_on_active_issue"
		out.SessionID = existing.SessionID
		_ = h.Integrations.AttachWebhookDeliverySession(
			ctx, deliveryID, existing.SessionID,
		)
		return out, nil
	}

	if perIssue && event.IssueKey != "" {
		existing, err := h.Integrations.GetGitHubIssueSession(
			ctx, pub.ID, event.IssueKey,
		)
		if err != nil {
			return out, err
		}
		if existing != nil && existing.Status == "active" {
			if err := h.Dispatch.ResumeUserMessage(
				ctx, existing.SessionID, userEvent,
			); err == nil {
				out.Handled = true
				out.Reason = "resumed_issue_session"
				out.SessionID = existing.SessionID
				_ = h.Integrations.AttachWebhookDeliverySession(
					ctx, deliveryID, existing.SessionID,
				)
				return out, nil
			}
		}
	}

	sessionID, err := h.Dispatch.DispatchUserMessage(ctx, DispatchInput{
		TenantID:      pub.TenantID,
		UserID:        pub.UserID,
		AgentID:       pub.AgentID,
		EnvironmentID: envID,
		Title:         title,
		UserEvent:     userEvent,
		PublicationID: pub.ID,
		IssueKey:      event.IssueKey,
		PerIssue:      perIssue,
	})
	if err != nil {
		out.Reason = "dispatch_failed"
		return out, err
	}
	if perIssue && event.IssueKey != "" {
		_ = h.Integrations.UpsertGitHubIssueSession(
			ctx, pub.ID, event.IssueKey, sessionID,
		)
	}
	out.Handled = true
	out.Reason = "session_created"
	out.SessionID = sessionID
	_ = h.Integrations.AttachWebhookDeliverySession(
		ctx, deliveryID, sessionID,
	)
	return out, nil
}
