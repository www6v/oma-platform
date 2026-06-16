package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// GitHubPublicationCredentials extends publication credentials for GitHub Apps.
type GitHubPublicationCredentials struct {
	AppID            string
	AppSlug          string
	BotLogin         string
	ClientID         string
	ClientSecret     string
	WebhookSecret    string
	PrivateKey       string
}

// GitHubCredentialState is the install-bridge view of a GitHub publication row.
type GitHubCredentialState struct {
	AppOmaID      string
	AppID         string
	AppSlug       string
	BotLogin      string
	HasPrivateKey bool
}

// InsertGitHubPublicationShell creates a pending_setup row with app_oma_id.
func (r *IntegrationRepo) InsertGitHubPublicationShell(
	ctx context.Context,
	id, appOmaID string,
	row NewPublicationShell,
) (*IntegrationPublication, error) {
	table := publicationsTable(ProviderGitHub)
	now := time.Now().Unix()
	caps, err := json.Marshal(row.Capabilities)
	if err != nil {
		return nil, err
	}
	mode := row.Mode
	if mode == "" {
		mode = "full"
	}
	gran := row.SessionGranularity
	if gran == "" {
		gran = defaultSessionGranularity(ProviderGitHub)
	}
	_, err = r.db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			id, tenant_id, user_id, agent_id, installation_id,
			environment_id, mode, status, persona_name, persona_avatar_url,
			capabilities, session_granularity, created_at, return_url,
			app_oma_id
		) VALUES (?, ?, ?, ?, '', ?, ?, 'pending_setup', ?, ?, ?, ?, ?, ?, ?)`,
		table),
		id, tenantOrDefault(row.TenantID), row.UserID, row.AgentID,
		nullIfEmpty(row.EnvironmentID), mode,
		row.PersonaName, row.PersonaAvatarURL, string(caps), gran, now,
		nullIfEmpty(row.ReturnURL), appOmaID,
	)
	if err != nil {
		return nil, fmt.Errorf("insert github publication shell: %w", err)
	}
	return r.GetPublication(ctx, ProviderGitHub, id)
}

// SetGitHubPublicationCredentials stores GitHub App credentials on a publication.
func (r *IntegrationRepo) SetGitHubPublicationCredentials(
	ctx context.Context,
	id string,
	creds GitHubPublicationCredentials,
) error {
	table := publicationsTable(ProviderGitHub)
	_, err := r.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s SET
			app_id = ?,
			app_slug = ?,
			bot_login = ?,
			client_id = ?,
			client_secret_cipher = ?,
			webhook_secret_cipher = ?,
			private_key_cipher = ?,
			status = 'credentials_filled'
		WHERE id = ? AND unpublished_at IS NULL`, table),
		creds.AppID, creds.AppSlug, creds.BotLogin,
		nullIfEmpty(creds.ClientID), nullIfEmpty(creds.ClientSecret),
		creds.WebhookSecret, creds.PrivateKey, id,
	)
	if err != nil {
		return fmt.Errorf("set github publication credentials: %w", err)
	}
	return nil
}

// GetGitHubCredentialState returns GitHub-specific credential columns.
func (r *IntegrationRepo) GetGitHubCredentialState(
	ctx context.Context,
	publicationID string,
) (*GitHubCredentialState, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT app_oma_id, app_id, app_slug, bot_login, private_key_cipher
		FROM github_publications
		WHERE id = ?`, publicationID)
	var state GitHubCredentialState
	var appOmaID, appID, appSlug, botLogin, privateKey *string
	err := row.Scan(&appOmaID, &appID, &appSlug, &botLogin, &privateKey)
	if err != nil {
		return nil, fmt.Errorf("get github credential state: %w", err)
	}
	if appOmaID != nil {
		state.AppOmaID = *appOmaID
	}
	if appID != nil {
		state.AppID = *appID
	}
	if appSlug != nil {
		state.AppSlug = *appSlug
	}
	if botLogin != nil {
		state.BotLogin = *botLogin
	}
	state.HasPrivateKey = privateKey != nil && *privateKey != ""
	return &state, nil
}

// GetGitHubPrivateKey returns the stored PEM private key for a publication.
func (r *IntegrationRepo) GetGitHubPrivateKey(
	ctx context.Context,
	publicationID string,
) (string, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT private_key_cipher FROM github_publications WHERE id = ?`,
		publicationID,
	)
	var key *string
	if err := row.Scan(&key); err != nil {
		return "", fmt.Errorf("get github private key: %w", err)
	}
	if key == nil {
		return "", nil
	}
	return *key, nil
}

// UpdatePublicationStatus sets the publication status column.
func (r *IntegrationRepo) UpdatePublicationStatus(
	ctx context.Context,
	provider IntegrationProvider,
	id, status string,
) error {
	table := publicationsTable(provider)
	_, err := r.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s SET status = ? WHERE id = ? AND unpublished_at IS NULL`,
		table), status, id)
	if err != nil {
		return fmt.Errorf("update publication status: %w", err)
	}
	return nil
}
