package installbridge

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/open-ma/oma-building/internal/integrations/github"
	"github.com/open-ma/oma-building/internal/integrations/oauthstate"
	"github.com/open-ma/oma-building/internal/integrations/slack"
	"github.com/open-ma/oma-building/internal/store"
)

// ContinueResult is returned after OAuth or manifest continuation.
type ContinueResult struct {
	PublicationID string
	ReturnURL     string
}

// ManifestForm holds data for the manifest auto-POST HTML page.
type ManifestForm struct {
	ManifestJSON string
	State        string
	PersonaName  string
}

// PrepareManifestForm validates formToken and builds manifest payload + state.
func (b *Bridge) PrepareManifestForm(
	formToken string,
) (ManifestForm, error) {
	var empty ManifestForm
	form, err := oauthstate.VerifyFormToken(
		b.Secret, formToken, "github.pub.form",
	)
	if err != nil {
		return empty, err
	}
	if form.PublicationID == "" || form.AppOmaID == "" {
		return empty, fmt.Errorf("formToken missing publicationId/appOmaId")
	}
	pub, err := b.Integrations.GetPublication(
		context.Background(), store.ProviderGitHub, form.PublicationID,
	)
	if err != nil {
		return empty, err
	}
	if pub == nil {
		return empty, fmt.Errorf("publication not found")
	}
	state, err := oauthstate.SignFormToken(b.Secret, oauthstate.FormTokenPayload{
		Kind:          "github.manifest.state",
		PublicationID: form.PublicationID,
		AppOmaID:      form.AppOmaID,
		UserID:        form.UserID,
		ReturnURL:     form.ReturnURL,
	}, formTokenTTL)
	if err != nil {
		return empty, err
	}
	manifest := github.BuildManifest(github.ManifestInput{
		Name:        pub.PersonaName,
		WebhookURL:  github.PublicationWebhookURI(b.Origin, pub.ID),
		RedirectURL: github.ManifestCallbackURI(b.Origin),
		SetupURL:    github.PublicationCallbackURI(b.Origin, pub.ID),
	})
	raw, err := json.Marshal(manifest)
	if err != nil {
		return empty, err
	}
	return ManifestForm{
		ManifestJSON: string(raw),
		State:        state,
		PersonaName:  pub.PersonaName,
	}, nil
}

// CompleteManifestCallback exchanges manifest code, stores creds, returns install URL.
func (b *Bridge) CompleteManifestCallback(
	ctx context.Context,
	code, stateToken string,
) (ContinueResult, error) {
	var empty ContinueResult
	if code == "" || stateToken == "" {
		return empty, fmt.Errorf("missing code or state")
	}
	state, err := oauthstate.VerifyFormToken(
		b.Secret, stateToken, "github.manifest.state",
	)
	if err != nil {
		return empty, err
	}
	conv, err := github.ExchangeManifestCode(b.HTTP, code)
	if err != nil {
		return empty, err
	}
	pub, err := b.loadResumablePublication(
		ctx, store.ProviderGitHub, state.PublicationID,
	)
	if err != nil {
		return empty, err
	}
	if err := b.Integrations.SetGitHubPublicationCredentials(
		ctx, pub.ID,
		store.GitHubPublicationCredentials{
			AppID:         fmt.Sprintf("%d", conv.ID),
			AppSlug:       conv.Slug,
			BotLogin:      conv.BotLogin,
			ClientID:      conv.ClientID,
			ClientSecret:  conv.ClientSecret,
			WebhookSecret: conv.WebhookSecret,
			PrivateKey:    conv.PEM,
		},
	); err != nil {
		return empty, err
	}
	if pub.Status == "pending_setup" || pub.Status == "credentials_filled" {
		_ = b.Integrations.UpdatePublicationStatus(
			ctx, store.ProviderGitHub, pub.ID, "awaiting_install",
		)
	}
	installState, err := oauthstate.SignFormToken(b.Secret, oauthstate.FormTokenPayload{
		Kind:          "github.install.pub",
		PublicationID: pub.ID,
		AppOmaID:      state.AppOmaID,
		UserID:        state.UserID,
		ReturnURL:     state.ReturnURL,
	}, formTokenTTL)
	if err != nil {
		return empty, err
	}
	installURL := github.BuildInstallURL(conv.Slug, installState)
	return ContinueResult{
		PublicationID: pub.ID,
		ReturnURL:     installURL,
	}, nil
}

// CompleteGitHubOAuth finishes GitHub App org install after setup callback.
func (b *Bridge) CompleteGitHubOAuth(
	ctx context.Context,
	publicationID, installationID, stateToken string,
) (ContinueResult, error) {
	var empty ContinueResult
	if publicationID == "" || installationID == "" || stateToken == "" {
		return empty, fmt.Errorf(
			"missing publicationId, installation_id, or state",
		)
	}
	state, err := oauthstate.VerifyFormToken(
		b.Secret, stateToken, "github.install.pub",
	)
	if err != nil {
		return empty, err
	}
	if state.PublicationID != publicationID {
		return empty, fmt.Errorf("publicationId mismatch")
	}
	pub, err := b.Integrations.GetPublication(
		ctx, store.ProviderGitHub, publicationID,
	)
	if err != nil {
		return empty, err
	}
	if pub == nil {
		return empty, fmt.Errorf("publication not found")
	}
	if pub.Status == "live" && pub.InstallationID != "" {
		return ContinueResult{
			PublicationID: pub.ID,
			ReturnURL:     state.ReturnURL,
		}, nil
	}
	cred, err := b.Integrations.GetGitHubCredentialState(ctx, publicationID)
	if err != nil {
		return empty, err
	}
	if cred == nil || cred.AppID == "" || cred.AppSlug == "" ||
		!cred.HasPrivateKey {
		return empty, fmt.Errorf(
			"publication has no credentials — re-paste before installing",
		)
	}
	privateKey, err := b.Integrations.GetGitHubPrivateKey(ctx, publicationID)
	if err != nil || privateKey == "" {
		return empty, fmt.Errorf("publication missing private key")
	}
	appJWT, err := github.MintAppJWT(privateKey, cred.AppID)
	if err != nil {
		return empty, err
	}
	if _, err := github.MintInstallationToken(
		b.HTTP, appJWT, installationID,
	); err != nil {
		return empty, fmt.Errorf("github install verification failed: %w", err)
	}
	detail, err := github.GetInstallation(b.HTTP, appJWT, installationID)
	if err != nil {
		return empty, err
	}
	appOmaID := state.AppOmaID
	if appOmaID == "" {
		appOmaID = cred.AppOmaID
	}
	instID := "gh_inst_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	var appIDPtr *string
	if appOmaID != "" {
		appIDPtr = &appOmaID
	}
	botLogin := cred.BotLogin
	if botLogin == "" {
		botLogin = cred.AppSlug + "[bot]"
	}
	inst, err := b.Integrations.InsertProviderInstallation(
		ctx, store.NewProviderInstallation{
			ID:            instID,
			TenantID:      pub.TenantID,
			UserID:        state.UserID,
			Provider:      store.ProviderGitHub,
			WorkspaceID:   installationID,
			WorkspaceName: detail.Account.Login,
			BotUserID:     botLogin,
			AppID:         appIDPtr,
		},
	)
	if err != nil {
		return empty, err
	}
	if err := b.Integrations.BindProviderPublication(
		ctx, store.ProviderGitHub, publicationID,
		store.BindProviderPublication{InstallationID: inst.ID},
	); err != nil {
		return empty, err
	}
	return ContinueResult{
		PublicationID: pub.ID,
		ReturnURL:     state.ReturnURL,
	}, nil
}

// CompleteSlackOAuth exchanges OAuth code and binds workspace installation.
func (b *Bridge) CompleteSlackOAuth(
	ctx context.Context,
	publicationID, code, stateToken string,
) (ContinueResult, error) {
	var empty ContinueResult
	if publicationID == "" || code == "" || stateToken == "" {
		return empty, fmt.Errorf("missing publicationId, code, or state")
	}
	state, err := oauthstate.VerifyFormToken(
		b.Secret, stateToken, "slack.oauth.pub",
	)
	if err != nil {
		return empty, err
	}
	if state.PublicationID != publicationID {
		return empty, fmt.Errorf("publicationId mismatch")
	}
	pub, err := b.Integrations.GetPublication(
		ctx, store.ProviderSlack, publicationID,
	)
	if err != nil {
		return empty, err
	}
	if pub == nil {
		return empty, fmt.Errorf("publication not found")
	}
	if pub.Status == "live" && pub.InstallationID != "" {
		return ContinueResult{
			PublicationID: pub.ID,
			ReturnURL:     state.ReturnURL,
		}, nil
	}
	creds, err := b.Integrations.GetPublicationCredentials(
		ctx, store.ProviderSlack, publicationID,
	)
	if err != nil {
		return empty, err
	}
	if creds == nil || creds.ClientID == "" || creds.ClientSecret == "" {
		return empty, fmt.Errorf(
			"publication has no client credentials — re-paste before installing",
		)
	}
	callback := slack.PublicationCallbackURI(b.Origin, publicationID)
	token, err := slack.ExchangeOAuthCode(
		b.HTTP, code, callback, creds.ClientID, creds.ClientSecret,
	)
	if err != nil {
		return empty, err
	}
	instID := "sl_inst_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	var appIDPtr *string
	if token.AppID != "" {
		appIDPtr = &token.AppID
	}
	inst, err := b.Integrations.InsertProviderInstallation(
		ctx, store.NewProviderInstallation{
			ID:            instID,
			TenantID:      pub.TenantID,
			UserID:        state.UserID,
			Provider:      store.ProviderSlack,
			WorkspaceID:   token.TeamID,
			WorkspaceName: token.TeamName,
			BotUserID:     token.BotUserID,
			AppID:         appIDPtr,
		},
	)
	if err != nil {
		return empty, err
	}
	if err := b.Integrations.BindProviderPublication(
		ctx, store.ProviderSlack, publicationID,
		store.BindProviderPublication{InstallationID: inst.ID},
	); err != nil {
		return empty, err
	}
	return ContinueResult{
		PublicationID: pub.ID,
		ReturnURL:     state.ReturnURL,
	}, nil
}
