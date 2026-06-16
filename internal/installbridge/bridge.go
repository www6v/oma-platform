package installbridge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/open-ma/oma-building/internal/integrations/github"
	"github.com/open-ma/oma-building/internal/integrations/oauthstate"
	"github.com/open-ma/oma-building/internal/integrations/slack"
	"github.com/open-ma/oma-building/internal/store"
)

const handoffTTL = 7 * 24 * time.Hour
const formTokenTTL = 60 * time.Minute

// Bridge implements in-process install proxy for GitHub and Slack.
type Bridge struct {
	Integrations *store.IntegrationRepo
	Origin       string
	Secret       string
	HTTP         *http.Client
}

// New returns a bridge when secret is configured.
func New(
	repo *store.IntegrationRepo,
	origin, secret string,
) *Bridge {
	if repo == nil || secret == "" {
		return nil
	}
	if origin == "" {
		origin = "http://127.0.0.1:8787"
	}
	return &Bridge{
		Integrations: repo,
		Origin:       strings.TrimRight(origin, "/"),
		Secret:       secret,
		HTTP:         &http.Client{Timeout: 30 * time.Second},
	}
}

// StartA1Input is the Console publish wizard payload.
type StartA1Input struct {
	UserID           string
	TenantID         string
	AgentID          string
	EnvironmentID    string
	PersonaName      string
	PersonaAvatarURL *string
	ReturnURL        string
}

// HandoffInput carries a form token for admin handoff links.
type HandoffInput struct {
	FormToken string `json:"formToken"`
}

// GitHubCredentialsInput is POST /credentials for GitHub.
type GitHubCredentialsInput struct {
	FormToken     string `json:"formToken"`
	AppID         string `json:"appId"`
	PrivateKey    string `json:"privateKey"`
	WebhookSecret string `json:"webhookSecret"`
	ClientID      string `json:"clientId"`
	ClientSecret  string `json:"clientSecret"`
}

// SlackCredentialsInput is POST /credentials for Slack.
type SlackCredentialsInput struct {
	FormToken     string `json:"formToken"`
	ClientID      string `json:"clientId"`
	ClientSecret  string `json:"clientSecret"`
	SigningSecret string `json:"signingSecret"`
}

// LinearLegacyRemoved is returned for deprecated Linear install endpoints.
var LinearLegacyRemoved = errors.New("linear_legacy_install_removed")

// StartA1 creates a publication shell and returns credentials_form JSON.
func (b *Bridge) StartA1(
	ctx context.Context,
	provider store.IntegrationProvider,
	in StartA1Input,
) (map[string]any, error) {
	if in.UserID == "" || in.AgentID == "" || in.EnvironmentID == "" ||
		in.PersonaName == "" || in.ReturnURL == "" {
		return nil, fmt.Errorf(
			"agentId, environmentId, personaName, returnUrl required",
		)
	}
	switch provider {
	case store.ProviderGitHub:
		return b.startGitHubA1(ctx, in)
	case store.ProviderSlack:
		return b.startSlackA1(ctx, in)
	default:
		return nil, fmt.Errorf("unsupported provider: %s", provider)
	}
}

func (b *Bridge) startGitHubA1(
	ctx context.Context,
	in StartA1Input,
) (map[string]any, error) {
	pubID := "pub_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	appOmaID := "ghapp_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	pub, err := b.Integrations.InsertGitHubPublicationShell(
		ctx, pubID, appOmaID,
		store.NewPublicationShell{
			TenantID:           in.TenantID,
			UserID:             in.UserID,
			AgentID:            in.AgentID,
			EnvironmentID:      in.EnvironmentID,
			PersonaName:        in.PersonaName,
			PersonaAvatarURL:   in.PersonaAvatarURL,
			Capabilities:       []string{},
			ReturnURL:          in.ReturnURL,
		},
	)
	if err != nil {
		return nil, err
	}
	token, err := b.signFormToken(oauthstate.FormTokenPayload{
		Kind:          "github.pub.form",
		PublicationID: pub.ID,
		AppOmaID:      appOmaID,
		UserID:        in.UserID,
		ReturnURL:     in.ReturnURL,
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"formToken":            token,
		"publicationId":        pub.ID,
		"appOmaId":             appOmaID,
		"suggestedAppName":     pub.PersonaName,
		"suggestedAvatarUrl":   pub.PersonaAvatarURL,
		"setupUrl":             github.PublicationCallbackURI(b.Origin, pub.ID),
		"webhookUrl":           github.PublicationWebhookURI(b.Origin, pub.ID),
		"manifestStartUrl":     b.Origin + "/github/manifest/start/" + token,
		"recommendedPermissions": map[string]string{
			"contents":       "write",
			"issues":         "write",
			"pull_requests":  "write",
			"metadata":       "read",
			"actions":        "read",
		},
		"recommendedSubscriptions": []string{
			"issues",
			"issue_comment",
			"pull_request",
			"pull_request_review",
			"pull_request_review_comment",
		},
	}, nil
}

func (b *Bridge) startSlackA1(
	ctx context.Context,
	in StartA1Input,
) (map[string]any, error) {
	pubID := "pub_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	pub, err := b.Integrations.InsertPublicationShell(
		ctx, store.ProviderSlack, pubID,
		store.NewPublicationShell{
			TenantID:         in.TenantID,
			UserID:           in.UserID,
			AgentID:          in.AgentID,
			EnvironmentID:    in.EnvironmentID,
			PersonaName:      in.PersonaName,
			PersonaAvatarURL: in.PersonaAvatarURL,
			Capabilities:     []string{},
			ReturnURL:        in.ReturnURL,
		},
	)
	if err != nil {
		return nil, err
	}
	token, err := b.signFormToken(oauthstate.FormTokenPayload{
		Kind:          "slack.pub.form",
		PublicationID: pub.ID,
		UserID:        in.UserID,
		ReturnURL:     in.ReturnURL,
	})
	if err != nil {
		return nil, err
	}
	callback := slack.PublicationCallbackURI(b.Origin, pub.ID)
	webhook := slack.PublicationWebhookURI(b.Origin, pub.ID)
	return map[string]any{
		"formToken":          token,
		"publicationId":      pub.ID,
		"suggestedAppName":   pub.PersonaName,
		"suggestedAvatarUrl": pub.PersonaAvatarURL,
		"callbackUrl":        callback,
		"webhookUrl":         webhook,
		"manifestLaunchUrl": slack.BuildManifestLaunchURL(
			pub.PersonaName, webhook, callback,
		),
	}, nil
}

// ReissueFormToken re-mints formToken for an existing pending publication.
func (b *Bridge) ReissueFormToken(
	ctx context.Context,
	provider store.IntegrationProvider,
	pub store.IntegrationPublication,
	userID, returnURL string,
) (map[string]any, error) {
	if returnURL == "" && pub.ReturnURL != nil {
		returnURL = *pub.ReturnURL
	}
	switch provider {
	case store.ProviderGitHub:
		state, err := b.Integrations.GetGitHubCredentialState(ctx, pub.ID)
		if err != nil {
			return nil, err
		}
		if state == nil || state.AppOmaID == "" {
			return nil, fmt.Errorf(
				"publication has no app_oma_id; restart the publish flow",
			)
		}
		token, err := b.signFormToken(oauthstate.FormTokenPayload{
			Kind:          "github.pub.form",
			PublicationID: pub.ID,
			AppOmaID:      state.AppOmaID,
			UserID:        userID,
			ReturnURL:     returnURL,
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"formToken":            token,
			"publicationId":        pub.ID,
			"appOmaId":             state.AppOmaID,
			"suggestedAppName":     pub.PersonaName,
			"suggestedAvatarUrl":   pub.PersonaAvatarURL,
			"setupUrl":             github.PublicationCallbackURI(b.Origin, pub.ID),
			"webhookUrl":           github.PublicationWebhookURI(b.Origin, pub.ID),
			"manifestStartUrl":     b.Origin + "/github/manifest/start/" + token,
			"recommendedPermissions": map[string]string{
				"contents": "write", "issues": "write",
				"pull_requests": "write", "metadata": "read", "actions": "read",
			},
			"recommendedSubscriptions": []string{
				"issues", "issue_comment", "pull_request",
				"pull_request_review", "pull_request_review_comment",
			},
		}, nil
	case store.ProviderSlack:
		token, err := b.signFormToken(oauthstate.FormTokenPayload{
			Kind:          "slack.pub.form",
			PublicationID: pub.ID,
			UserID:        userID,
			ReturnURL:     returnURL,
		})
		if err != nil {
			return nil, err
		}
		callback := slack.PublicationCallbackURI(b.Origin, pub.ID)
		webhook := slack.PublicationWebhookURI(b.Origin, pub.ID)
		return map[string]any{
			"formToken":          token,
			"publicationId":      pub.ID,
			"suggestedAppName":   pub.PersonaName,
			"suggestedAvatarUrl": pub.PersonaAvatarURL,
			"callbackUrl":        callback,
			"webhookUrl":         webhook,
			"manifestLaunchUrl": slack.BuildManifestLaunchURL(
				pub.PersonaName, webhook, callback,
			),
		}, nil
	default:
		return nil, fmt.Errorf("form token reissue unsupported for %s", provider)
	}
}

// SubmitGitHubCredentials validates App creds and returns install_link JSON.
func (b *Bridge) SubmitGitHubCredentials(
	ctx context.Context,
	in GitHubCredentialsInput,
) (map[string]any, error) {
	form, err := oauthstate.VerifyFormToken(
		b.Secret, in.FormToken, "github.pub.form",
	)
	if err != nil {
		return nil, err
	}
	if in.FormToken == "" || in.AppID == "" ||
		strings.TrimSpace(in.PrivateKey) == "" ||
		strings.TrimSpace(in.WebhookSecret) == "" {
		return nil, fmt.Errorf(
			"formToken, appId, privateKey, webhookSecret required",
		)
	}
	pub, err := b.loadResumablePublication(
		ctx, store.ProviderGitHub, form.PublicationID,
	)
	if err != nil {
		return nil, err
	}
	appJWT, err := github.MintAppJWT(strings.TrimSpace(in.PrivateKey), in.AppID)
	if err != nil {
		return nil, fmt.Errorf("invalid private key: %w", err)
	}
	appInfo, err := github.GetApp(b.HTTP, appJWT)
	if err != nil {
		return nil, fmt.Errorf("github app verification failed: %w", err)
	}
	if appInfo.ID != in.AppID {
		return nil, fmt.Errorf(
			"appId mismatch — pasted %s, GitHub says %s",
			in.AppID, appInfo.ID,
		)
	}
	if err := b.Integrations.SetGitHubPublicationCredentials(
		ctx, pub.ID,
		store.GitHubPublicationCredentials{
			AppID:         appInfo.ID,
			AppSlug:       appInfo.Slug,
			BotLogin:      appInfo.BotLogin,
			ClientID:      strings.TrimSpace(in.ClientID),
			ClientSecret:  strings.TrimSpace(in.ClientSecret),
			WebhookSecret: strings.TrimSpace(in.WebhookSecret),
			PrivateKey:    strings.TrimSpace(in.PrivateKey),
		},
	); err != nil {
		return nil, err
	}
	if pub.Status == "pending_setup" || pub.Status == "credentials_filled" ||
		pub.Status == "needs_reauth" {
		_ = b.Integrations.UpdatePublicationStatus(
			ctx, store.ProviderGitHub, pub.ID, "awaiting_install",
		)
	}
	state, err := oauthstate.SignFormToken(b.Secret, oauthstate.FormTokenPayload{
		Kind:          "github.install.pub",
		PublicationID: pub.ID,
		AppOmaID:      form.AppOmaID,
		UserID:        form.UserID,
		ReturnURL:     form.ReturnURL,
	}, formTokenTTL)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"url":            github.BuildInstallURL(appInfo.Slug, state),
		"publicationId":  pub.ID,
		"appOmaId":       form.AppOmaID,
		"appSlug":        appInfo.Slug,
		"botLogin":       appInfo.BotLogin,
		"setupUrl":       github.PublicationCallbackURI(b.Origin, pub.ID),
		"webhookUrl":     github.PublicationWebhookURI(b.Origin, pub.ID),
	}, nil
}

// SubmitSlackCredentials stores Slack app creds and returns install_link JSON.
func (b *Bridge) SubmitSlackCredentials(
	ctx context.Context,
	in SlackCredentialsInput,
) (map[string]any, error) {
	form, err := oauthstate.VerifyFormToken(
		b.Secret, in.FormToken, "slack.pub.form",
	)
	if err != nil {
		return nil, err
	}
	if in.FormToken == "" || strings.TrimSpace(in.ClientID) == "" ||
		strings.TrimSpace(in.ClientSecret) == "" ||
		strings.TrimSpace(in.SigningSecret) == "" {
		return nil, fmt.Errorf(
			"formToken, clientId, clientSecret, signingSecret required",
		)
	}
	pub, err := b.loadResumablePublication(
		ctx, store.ProviderSlack, form.PublicationID,
	)
	if err != nil {
		return nil, err
	}
	if pub.Status == "live" || pub.Status == "unpublished" {
		return nil, fmt.Errorf(
			"publication is '%s', credentials cannot be re-pasted", pub.Status,
		)
	}
	signing := strings.TrimSpace(in.SigningSecret)
	if err := b.Integrations.SetPublicationCredentials(
		ctx, store.ProviderSlack, pub.ID,
		store.PublicationCredentials{
			ClientID:      strings.TrimSpace(in.ClientID),
			ClientSecret:  strings.TrimSpace(in.ClientSecret),
			SigningSecret: &signing,
		},
	); err != nil {
		return nil, err
	}
	if pub.Status == "pending_setup" || pub.Status == "needs_reauth" {
		_ = b.Integrations.UpdatePublicationStatus(
			ctx, store.ProviderSlack, pub.ID, "awaiting_install",
		)
	}
	state, err := oauthstate.SignFormToken(b.Secret, oauthstate.FormTokenPayload{
		Kind:          "slack.oauth.pub",
		PublicationID: pub.ID,
		UserID:        form.UserID,
		ReturnURL:     form.ReturnURL,
	}, formTokenTTL)
	if err != nil {
		return nil, err
	}
	callback := slack.PublicationCallbackURI(b.Origin, pub.ID)
	return map[string]any{
		"url": slack.BuildAuthorizeURL(
			strings.TrimSpace(in.ClientID), callback, state, nil, nil,
		),
		"publicationId": pub.ID,
		"callbackUrl":   callback,
		"webhookUrl":    slack.PublicationWebhookURI(b.Origin, pub.ID),
	}, nil
}

// CreateHandoffLink returns a 7-day admin setup URL.
func (b *Bridge) CreateHandoffLink(
	provider store.IntegrationProvider,
	in HandoffInput,
) (map[string]any, error) {
	if in.FormToken == "" {
		return nil, fmt.Errorf("formToken required")
	}
	var kind, pathPrefix string
	switch provider {
	case store.ProviderGitHub:
		kind = "github.pub.form"
		pathPrefix = "/github-setup/"
	case store.ProviderSlack:
		kind = "slack.pub.form"
		pathPrefix = "/slack-setup/"
	default:
		return nil, fmt.Errorf("handoff unsupported for %s", provider)
	}
	form, err := oauthstate.VerifyFormToken(b.Secret, in.FormToken, kind)
	if err != nil {
		return nil, err
	}
	form.Handoff = true
	handoffToken, err := oauthstate.SignFormToken(
		b.Secret, form, handoffTTL,
	)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"url":           b.Origin + pathPrefix + handoffToken,
		"expiresInDays": 7,
	}, nil
}

func (b *Bridge) signFormToken(payload oauthstate.FormTokenPayload) (string, error) {
	return oauthstate.SignFormToken(b.Secret, payload, formTokenTTL)
}

func (b *Bridge) loadResumablePublication(
	ctx context.Context,
	provider store.IntegrationProvider,
	id string,
) (*store.IntegrationPublication, error) {
	pub, err := b.Integrations.GetPublication(ctx, provider, id)
	if err != nil {
		return nil, err
	}
	if pub == nil {
		return nil, fmt.Errorf("publication not found")
	}
	if pub.Status == "unpublished" {
		return nil, fmt.Errorf("publication is unpublished")
	}
	return pub, nil
}

// Proxy forwards install requests to an external integrations worker.
type Proxy struct {
	BaseURL string
	Secret  string
	Client  *http.Client
}

// NewProxy returns an HTTP forwarder when base URL is set.
func NewProxy(baseURL, secret string) *Proxy {
	if strings.TrimSpace(baseURL) == "" {
		return nil
	}
	return &Proxy{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Secret:  secret,
		Client:  &http.Client{Timeout: 60 * time.Second},
	}
}

// Forward posts JSON to the external worker and returns parsed body + status.
func (p *Proxy) Forward(
	ctx context.Context,
	subpath string,
	body map[string]any,
	needsSecret bool,
) (int, json.RawMessage, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, p.BaseURL+"/"+subpath, bytes.NewReader(raw),
	)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if needsSecret && p.Secret != "" {
		req.Header.Set("x-internal-secret", p.Secret)
	}
	resp, err := p.Client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	var out json.RawMessage
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&out); err != nil {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, out, nil
}
