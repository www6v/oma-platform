package slack

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/open-ma/oma-building/internal/store"
)

// WebhookOutcome describes how a webhook delivery was handled.
type WebhookOutcome struct {
	Handled         bool
	Reason          string
	SessionID       string
	PublicationID   string
	TenantID        string
	URLVerification bool
	Challenge       string
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
	ScopeKey      string
	PerThread     bool
}

// Handler orchestrates Slack webhook dispatch.
type Handler struct {
	Integrations *store.IntegrationRepo
	Origin       string
	Dispatch     SessionDispatcher
}

// PublicationWebhookURI builds the webhook URL surfaced to Slack admins.
func PublicationWebhookURI(origin, pubID string) string {
	return strings.TrimRight(origin, "/") +
		"/slack/webhook/pub/" + pubID
}

// BindMockInstallation completes a publication without calling Slack APIs.
func (h *Handler) BindMockInstallation(
	ctx context.Context,
	pubID, workspaceID, workspaceName, botUserID string,
) error {
	pub, err := h.Integrations.GetPublication(ctx, store.ProviderSlack, pubID)
	if err != nil {
		return err
	}
	if pub == nil {
		return fmt.Errorf("unknown publication")
	}
	if pub.Status == "live" {
		return nil
	}
	instID := "sl_inst_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	inst, err := h.Integrations.InsertProviderInstallation(ctx, store.NewProviderInstallation{
		ID:            instID,
		TenantID:      pub.TenantID,
		UserID:        pub.UserID,
		Provider:      store.ProviderSlack,
		WorkspaceID:   workspaceID,
		WorkspaceName: workspaceName,
		BotUserID:     botUserID,
	})
	if err != nil {
		return err
	}
	return h.Integrations.BindProviderPublication(
		ctx, store.ProviderSlack, pubID, store.BindProviderPublication{
			InstallationID: inst.ID,
		},
	)
}

// HandleWebhook verifies, parses, and dispatches a Slack webhook delivery.
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

	pub, err := h.Integrations.GetPublication(ctx, store.ProviderSlack, pubID)
	if err != nil {
		return out, err
	}
	if pub == nil {
		out.Reason = "publication_not_found"
		return out, nil
	}
	out.PublicationID = pub.ID
	out.TenantID = pub.TenantID

	secret, err := h.Integrations.GetPublicationSigningSecret(
		ctx, store.ProviderSlack, pubID,
	)
	if err != nil {
		return out, err
	}
	if secret == "" {
		out.Reason = "missing_signing_secret"
		return out, nil
	}
	timestamp := headers["x-slack-request-timestamp"]
	signature := headers["x-slack-signature"]
	if timestamp == "" || signature == "" {
		out.Reason = "missing_signature"
		return out, nil
	}
	if !VerifyWebhookSignature(secret, rawBody, timestamp, signature) {
		out.Reason = "invalid_signature"
		return out, nil
	}

	parsed, err := ParseWebhook(rawBody)
	if err != nil {
		out.Reason = "invalid_json"
		return out, nil
	}
	if parsed.URLVerification {
		out.Handled = true
		out.Reason = "url_verification"
		out.URLVerification = true
		out.Challenge = parsed.Challenge
		return out, nil
	}
	if parsed.Event == nil || parsed.Event.Kind == "" {
		out.Reason = "ignored_event"
		return out, nil
	}
	event := parsed.Event
	if deliveryID == "" {
		deliveryID = event.DeliveryID
	}
	if deliveryID == "" {
		out.Reason = "missing_delivery_id"
		return out, nil
	}
	if pub.Status == "unpublished" {
		out.Reason = "publication_unpublished"
		return out, nil
	}
	if pub.InstallationID == "" {
		out.Reason = "publication_not_live"
		return out, nil
	}
	inst, err := h.Integrations.GetInstallation(
		ctx, store.ProviderSlack, pub.InstallationID,
	)
	if err != nil {
		return out, err
	}
	if inst == nil || inst.RevokedAt != nil {
		out.Reason = "installation_not_found_or_revoked"
		return out, nil
	}

	fresh, err := h.Integrations.RecordWebhookDeliveryIfNew(
		ctx, deliveryID, "slack", pubID, inst.ID,
	)
	if err != nil {
		return out, err
	}
	if !fresh {
		out.Reason = "duplicate_delivery"
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
	title := "Slack mention"
	if event.Text != "" {
		title = event.Text
		if len(title) > 80 {
			title = title[:80]
		}
	}

	perThread := pub.SessionGranularity == "per_thread" && event.ScopeKey != ""
	if perThread {
		existing, err := h.Integrations.GetSlackScopeSession(
			ctx, pub.ID, event.ScopeKey,
		)
		if err != nil {
			return out, err
		}
		if existing != nil && existing.Status == "active" {
			if err := h.Dispatch.ResumeUserMessage(
				ctx, existing.SessionID, userEvent,
			); err == nil {
				out.Handled = true
				out.Reason = "resumed_thread_session"
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
		ScopeKey:      event.ScopeKey,
		PerThread:     perThread,
	})
	if err != nil {
		out.Reason = "dispatch_failed"
		return out, err
	}
	if perThread && event.ScopeKey != "" {
		_ = h.Integrations.UpsertSlackScopeSession(
			ctx, pub.ID, event.ScopeKey, sessionID,
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
