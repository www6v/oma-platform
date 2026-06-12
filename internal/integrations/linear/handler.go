package linear

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/open-ma/oma-building/internal/integrations/oauthstate"
	"github.com/open-ma/oma-building/internal/store"
)

// OAuthCompleteResult is returned after a successful OAuth callback.
type OAuthCompleteResult struct {
	PublicationID string
	ReturnURL     string
	AlreadyLive   bool
}

// WebhookOutcome describes how a webhook delivery was handled.
type WebhookOutcome struct {
	Handled       bool
	Reason        string
	SessionID     string
	PublicationID string
	TenantID      string
}

// Handler orchestrates Linear OAuth and webhook dispatch.
type Handler struct {
	Integrations *store.IntegrationRepo
	HTTP         HTTPDoer
	Origin       string
	StateSecret  string
	Dispatch     SessionDispatcher
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
	IssueID       string
	PerIssue      bool
}

// PublicationCallbackURI builds the OAuth redirect URI for a publication.
func PublicationCallbackURI(origin, pubID string) string {
	return strings.TrimRight(origin, "/") +
		"/linear/oauth/pub/" + pubID + "/callback"
}

// PublicationWebhookURI builds the webhook URL surfaced to Linear admins.
func PublicationWebhookURI(origin, pubID string) string {
	return strings.TrimRight(origin, "/") +
		"/linear/webhook/pub/" + pubID
}

// BuildInstallURL returns the local authorize URL for a publication.
func (h *Handler) BuildInstallURL(pubID, returnURL string) (string, error) {
	state, err := oauthstate.SignLinearPublication(h.StateSecret, oauthstate.LinearPublicationPayload{
		PublicationID: pubID,
		ReturnURL:     returnURL,
		Nonce:         uuid.NewString(),
	})
	if err != nil {
		return "", err
	}
	return strings.TrimRight(h.Origin, "/") +
		"/linear/oauth/pub/" + pubID + "/authorize?state=" + state, nil
}

// AuthorizeRedirectURL builds the Linear OAuth URL for a publication.
func (h *Handler) AuthorizeRedirectURL(
	ctx context.Context,
	pubID, stateToken string,
) (string, error) {
	state, err := oauthstate.VerifyLinearPublication(h.StateSecret, stateToken)
	if err != nil {
		return "", err
	}
	if state.PublicationID != pubID {
		return "", fmt.Errorf("state publication mismatch")
	}
	creds, err := h.Integrations.GetPublicationCredentials(
		ctx, store.ProviderLinear, pubID,
	)
	if err != nil {
		return "", err
	}
	if creds == nil {
		return "", fmt.Errorf("publication has no credentials")
	}
	redirectURI := PublicationCallbackURI(h.Origin, pubID)
	return BuildAuthorizeURL(
		creds.ClientID, redirectURI, stateToken, DefaultScopes,
	), nil
}

// CompleteOAuth exchanges the code and binds the publication live.
func (h *Handler) CompleteOAuth(
	ctx context.Context,
	pubID, code, stateToken string,
) (OAuthCompleteResult, error) {
	var result OAuthCompleteResult
	state, err := oauthstate.VerifyLinearPublication(h.StateSecret, stateToken)
	if err != nil {
		return result, err
	}
	if state.PublicationID != pubID {
		return result, fmt.Errorf("state publication mismatch")
	}
	result.ReturnURL = state.ReturnURL

	pub, err := h.Integrations.GetPublication(ctx, store.ProviderLinear, pubID)
	if err != nil {
		return result, err
	}
	if pub == nil {
		return result, fmt.Errorf("unknown publication")
	}
	if pub.Status == "live" {
		result.PublicationID = pub.ID
		result.AlreadyLive = true
		return result, nil
	}
	creds, err := h.Integrations.GetPublicationCredentials(
		ctx, store.ProviderLinear, pubID,
	)
	if err != nil {
		return result, err
	}
	if creds == nil {
		return result, fmt.Errorf("publication has no credentials")
	}
	redirectURI := PublicationCallbackURI(h.Origin, pubID)
	token, err := ExchangeAuthorizationCode(
		h.HTTP, code, redirectURI, creds.ClientID, creds.ClientSecret,
	)
	if err != nil {
		return result, err
	}
	viewer, org, err := FetchViewerAndOrg(h.HTTP, token.AccessToken)
	if err != nil {
		return result, err
	}
	existing, err := h.Integrations.FindLinearInstallationByWorkspace(ctx, org.ID)
	if err != nil {
		return result, err
	}
	if existing != nil {
		return result, fmt.Errorf(
			"workspace %s already has install %s",
			org.Name, existing.ID,
		)
	}
	instID := "lin_inst_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	inst, err := h.Integrations.InsertLinearInstallation(ctx, store.NewLinearInstallation{
		ID:            instID,
		TenantID:      pub.TenantID,
		UserID:        pub.UserID,
		WorkspaceID:   org.ID,
		WorkspaceName: org.Name,
		BotUserID:     viewer.ID,
	})
	if err != nil {
		return result, err
	}
	if err := h.Integrations.BindLinearPublication(ctx, pubID, store.BindLinearPublication{
		InstallationID: inst.ID,
	}); err != nil {
		return result, err
	}
	result.PublicationID = pubID
	return result, nil
}

// BindMockInstallation completes a publication without calling Linear APIs.
func (h *Handler) BindMockInstallation(
	ctx context.Context,
	pubID, workspaceID, workspaceName, botUserID string,
) error {
	pub, err := h.Integrations.GetPublication(ctx, store.ProviderLinear, pubID)
	if err != nil {
		return err
	}
	if pub == nil {
		return fmt.Errorf("unknown publication")
	}
	if pub.Status == "live" {
		return nil
	}
	instID := "lin_inst_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	inst, err := h.Integrations.InsertLinearInstallation(ctx, store.NewLinearInstallation{
		ID:            instID,
		TenantID:      pub.TenantID,
		UserID:        pub.UserID,
		WorkspaceID:   workspaceID,
		WorkspaceName: workspaceName,
		BotUserID:     botUserID,
	})
	if err != nil {
		return err
	}
	return h.Integrations.BindLinearPublication(ctx, pubID, store.BindLinearPublication{
		InstallationID: inst.ID,
	})
}

// HandleWebhook verifies, parses, and dispatches a Linear webhook delivery.
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

	pub, err := h.Integrations.GetPublication(ctx, store.ProviderLinear, pubID)
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
		ctx, store.ProviderLinear, pub.InstallationID,
	)
	if err != nil {
		return out, err
	}
	if inst == nil || inst.RevokedAt != nil {
		out.Reason = "installation_not_found_or_revoked"
		return out, nil
	}

	secret, err := h.Integrations.GetPublicationWebhookSecret(
		ctx, store.ProviderLinear, pubID,
	)
	if err != nil {
		return out, err
	}
	if secret == "" {
		out.Reason = "missing_webhook_secret"
		return out, nil
	}
	signature := headers["linear-signature"]
	if signature == "" {
		out.Reason = "missing_signature"
		return out, nil
	}
	if !VerifyWebhookSignature(secret, string(rawBody), signature) {
		out.Reason = "invalid_signature"
		return out, nil
	}

	fresh, err := h.Integrations.RecordWebhookDeliveryIfNew(
		ctx, deliveryID, "linear", pubID, inst.ID,
	)
	if err != nil {
		return out, err
	}
	if !fresh {
		out.Reason = "duplicate_delivery"
		return out, nil
	}

	event, err := ParseWebhook(rawBody)
	if err != nil {
		out.Reason = "invalid_json"
		return out, nil
	}
	if event == nil {
		out.Reason = "unparseable"
		return out, nil
	}
	if event.Kind == "" {
		out.Reason = "ignored_event_" + event.EventType
		return out, nil
	}
	if event.Kind == KindAgentSessionCreated {
		out.Handled = true
		out.Reason = "agent_session_created_panel_only"
		return out, nil
	}
	if event.Kind == KindCommentReply {
		if event.ActorUserID != "" && inst.BotUserID == event.ActorUserID {
			out.Reason = "comment_from_bot_self"
			return out, nil
		}
		if event.IssueID == "" {
			out.Reason = "comment_without_issue"
			return out, nil
		}
		existing, err := h.Integrations.GetLinearIssueSession(
			ctx, pub.ID, event.IssueID,
		)
		if err != nil {
			return out, err
		}
		if existing == nil || existing.Status != "active" {
			out.Reason = "comment_on_issue_with_no_active_session"
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
	title := event.IssueIdentifier
	if title == "" {
		title = "Linear event"
	}
	if event.IssueTitle != "" {
		title = event.IssueTitle
	}

	perIssue := pub.SessionGranularity == "per_issue" && event.IssueID != ""
	if perIssue {
		existing, err := h.Integrations.GetLinearIssueSession(
			ctx, pub.ID, event.IssueID,
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
		IssueID:       event.IssueID,
		PerIssue:      perIssue,
	})
	if err != nil {
		out.Reason = "dispatch_failed"
		return out, err
	}
	if perIssue && event.IssueID != "" {
		_ = h.Integrations.UpsertLinearIssueSession(
			ctx, pub.ID, event.IssueID, sessionID,
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
