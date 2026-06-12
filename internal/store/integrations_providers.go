package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

// GitHubIssueSession maps a repo#issue to a session for a publication.
type GitHubIssueSession struct {
	PublicationID string
	IssueKey      string
	SessionID     string
	Status        string
	CreatedAt     int64
}

// SlackScopeSession maps a channel/thread scope to a session.
type SlackScopeSession struct {
	PublicationID string
	ScopeKey      string
	SessionID     string
	Status        string
	CreatedAt     int64
}

// NewProviderInstallation input for mock/OAuth install completion.
type NewProviderInstallation struct {
	ID            string
	TenantID      string
	UserID        string
	Provider      IntegrationProvider
	WorkspaceID   string
	WorkspaceName string
	BotUserID     string
	AppID         *string
}

// BindProviderPublication links install and marks publication live.
type BindProviderPublication struct {
	InstallationID string
	VaultID        *string
}

// GetPublicationCredentials returns decrypted credentials for any provider.
func (r *IntegrationRepo) GetPublicationCredentials(
	ctx context.Context,
	provider IntegrationProvider,
	publicationID string,
) (*PublicationCredentials, error) {
	pub, err := r.GetPublication(ctx, provider, publicationID)
	if err != nil {
		return nil, err
	}
	if pub == nil {
		return nil, nil
	}
	if pub.ClientID == nil || pub.ClientSecretCipher == nil {
		return nil, nil
	}
	creds := &PublicationCredentials{
		ClientID:     *pub.ClientID,
		ClientSecret: *pub.ClientSecretCipher,
	}
	if pub.WebhookSecretCipher != nil {
		creds.WebhookSecret = *pub.WebhookSecretCipher
	}
	if pub.SigningSecretCipher != nil {
		creds.SigningSecret = pub.SigningSecretCipher
	}
	return creds, nil
}

// GetPublicationWebhookSecret returns GitHub/Linear webhook HMAC secret.
func (r *IntegrationRepo) GetPublicationWebhookSecret(
	ctx context.Context,
	provider IntegrationProvider,
	publicationID string,
) (string, error) {
	pub, err := r.GetPublication(ctx, provider, publicationID)
	if err != nil {
		return "", err
	}
	if pub == nil || pub.WebhookSecretCipher == nil {
		return "", nil
	}
	return *pub.WebhookSecretCipher, nil
}

// GetPublicationSigningSecret returns Slack Events API signing secret.
func (r *IntegrationRepo) GetPublicationSigningSecret(
	ctx context.Context,
	provider IntegrationProvider,
	publicationID string,
) (string, error) {
	pub, err := r.GetPublication(ctx, provider, publicationID)
	if err != nil {
		return "", err
	}
	if pub == nil || pub.SigningSecretCipher == nil {
		return "", nil
	}
	return *pub.SigningSecretCipher, nil
}

// InsertProviderInstallation creates a workspace installation row.
func (r *IntegrationRepo) InsertProviderInstallation(
	ctx context.Context,
	row NewProviderInstallation,
) (*IntegrationInstallation, error) {
	table := installationsTable(row.Provider)
	now := time.Now().Unix()
	_, err := r.db.ExecContext(ctx, fmt.Sprintf(`
		INSERT INTO %s (
			id, tenant_id, user_id, provider_id, workspace_id,
			workspace_name, install_kind, app_id, bot_user_id, vault_id,
			created_at
		) VALUES (?, ?, ?, ?, ?, ?, 'dedicated', ?, ?, NULL, ?)`, table),
		row.ID, tenantOrDefault(row.TenantID), row.UserID, string(row.Provider),
		row.WorkspaceID, row.WorkspaceName, row.AppID, row.BotUserID, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert installation: %w", err)
	}
	return r.GetInstallation(ctx, row.Provider, row.ID)
}

// BindProviderPublication links installation and marks publication live.
func (r *IntegrationRepo) BindProviderPublication(
	ctx context.Context,
	provider IntegrationProvider,
	publicationID string,
	bind BindProviderPublication,
) error {
	table := publicationsTable(provider)
	var vault any
	if bind.VaultID != nil {
		vault = *bind.VaultID
	}
	_, err := r.db.ExecContext(ctx, fmt.Sprintf(`
		UPDATE %s SET
			installation_id = ?,
			vault_id = ?,
			status = 'live'
		WHERE id = ? AND unpublished_at IS NULL`, table),
		bind.InstallationID, vault, publicationID,
	)
	if err != nil {
		return fmt.Errorf("bind publication: %w", err)
	}
	return nil
}

// PublicationTriggerLabel returns the default GitHub engagement label.
func PublicationTriggerLabel(personaName string) string {
	return strings.ToLower(strings.TrimSpace(personaName))
}

// GetGitHubIssueSession loads issue routing state.
func (r *IntegrationRepo) GetGitHubIssueSession(
	ctx context.Context,
	publicationID, issueKey string,
) (*GitHubIssueSession, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT publication_id, issue_key, session_id, status, created_at
		FROM github_issue_sessions
		WHERE publication_id = ? AND issue_key = ?`,
		publicationID, issueKey,
	)
	var item GitHubIssueSession
	err := row.Scan(
		&item.PublicationID, &item.IssueKey, &item.SessionID,
		&item.Status, &item.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get github issue session: %w", err)
	}
	return &item, nil
}

// UpsertGitHubIssueSession binds issue to session.
func (r *IntegrationRepo) UpsertGitHubIssueSession(
	ctx context.Context,
	publicationID, issueKey, sessionID string,
) error {
	now := time.Now().UnixMilli()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO github_issue_sessions (
			publication_id, issue_key, session_id, status, created_at
		) VALUES (?, ?, ?, 'active', ?)
		ON CONFLICT(publication_id, issue_key) DO UPDATE SET
			session_id = excluded.session_id,
			status = 'active'`,
		publicationID, issueKey, sessionID, now,
	)
	if err != nil {
		return fmt.Errorf("upsert github issue session: %w", err)
	}
	return nil
}

// GetSlackScopeSession loads channel/thread routing state.
func (r *IntegrationRepo) GetSlackScopeSession(
	ctx context.Context,
	publicationID, scopeKey string,
) (*SlackScopeSession, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT publication_id, scope_key, session_id, status, created_at
		FROM slack_scope_sessions
		WHERE publication_id = ? AND scope_key = ?`,
		publicationID, scopeKey,
	)
	var item SlackScopeSession
	err := row.Scan(
		&item.PublicationID, &item.ScopeKey, &item.SessionID,
		&item.Status, &item.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get slack scope session: %w", err)
	}
	return &item, nil
}

// UpsertSlackScopeSession binds scope to session.
func (r *IntegrationRepo) UpsertSlackScopeSession(
	ctx context.Context,
	publicationID, scopeKey, sessionID string,
) error {
	now := time.Now().UnixMilli()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO slack_scope_sessions (
			publication_id, scope_key, session_id, status, created_at
		) VALUES (?, ?, ?, 'active', ?)
		ON CONFLICT(publication_id, scope_key) DO UPDATE SET
			session_id = excluded.session_id,
			status = 'active'`,
		publicationID, scopeKey, sessionID, now,
	)
	if err != nil {
		return fmt.Errorf("upsert slack scope session: %w", err)
	}
	return nil
}
