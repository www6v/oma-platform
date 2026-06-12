package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// LinearIssueSession maps a Linear issue to an OMA session for a publication.
type LinearIssueSession struct {
	PublicationID string
	IssueID       string
	SessionID     string
	Status        string
	CreatedAt     int64
}

// NewLinearInstallation input for OAuth completion.
type NewLinearInstallation struct {
	ID            string
	TenantID      string
	UserID        string
	WorkspaceID   string
	WorkspaceName string
	BotUserID     string
}

// BindLinearPublication input for flipping a publication live.
type BindLinearPublication struct {
	InstallationID string
	VaultID        *string
}

// RecordWebhookDeliveryIfNew inserts a delivery idempotency row. Returns false
// when the delivery was already seen.
func (r *IntegrationRepo) RecordWebhookDeliveryIfNew(
	ctx context.Context,
	deliveryID, providerID, publicationID, installationID string,
) (bool, error) {
	now := time.Now().UnixMilli()
	res, err := r.db.ExecContext(ctx, `
		INSERT OR IGNORE INTO integration_webhook_deliveries (
			delivery_id, provider_id, publication_id, installation_id,
			received_at
		) VALUES (?, ?, ?, ?, ?)`,
		deliveryID, providerID, publicationID, installationID, now,
	)
	if err != nil {
		return false, fmt.Errorf("record webhook delivery: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// AttachWebhookDeliverySession records the session created for a delivery.
func (r *IntegrationRepo) AttachWebhookDeliverySession(
	ctx context.Context,
	deliveryID, sessionID string,
) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE integration_webhook_deliveries
		SET session_id = ?
		WHERE delivery_id = ?`, sessionID, deliveryID,
	)
	if err != nil {
		return fmt.Errorf("attach webhook session: %w", err)
	}
	return nil
}

// FindLinearInstallationByWorkspace returns a live dedicated install, if any.
func (r *IntegrationRepo) FindLinearInstallationByWorkspace(
	ctx context.Context,
	workspaceID string,
) (*IntegrationInstallation, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, user_id, provider_id, workspace_id,
			workspace_name, install_kind, app_id, bot_user_id, vault_id,
			created_at, revoked_at
		FROM linear_installations
		WHERE workspace_id = ? AND install_kind = 'dedicated'
			AND revoked_at IS NULL
		LIMIT 1`, workspaceID)
	item, err := scanInstallation(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find linear installation: %w", err)
	}
	return item, nil
}

// InsertLinearInstallation creates a workspace installation row.
func (r *IntegrationRepo) InsertLinearInstallation(
	ctx context.Context,
	row NewLinearInstallation,
) (*IntegrationInstallation, error) {
	now := time.Now().Unix()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO linear_installations (
			id, tenant_id, user_id, provider_id, workspace_id,
			workspace_name, install_kind, app_id, bot_user_id, vault_id,
			created_at
		) VALUES (?, ?, ?, 'linear', ?, ?, 'dedicated', NULL, ?, NULL, ?)`,
		row.ID, tenantOrDefault(row.TenantID), row.UserID,
		row.WorkspaceID, row.WorkspaceName, row.BotUserID, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert linear installation: %w", err)
	}
	return r.GetInstallation(ctx, ProviderLinear, row.ID)
}

// BindLinearPublication links an installation and marks the publication live.
func (r *IntegrationRepo) BindLinearPublication(
	ctx context.Context,
	publicationID string,
	bind BindLinearPublication,
) error {
	var vault any
	if bind.VaultID != nil {
		vault = *bind.VaultID
	}
	_, err := r.db.ExecContext(ctx, `
		UPDATE linear_publications SET
			installation_id = ?,
			vault_id = ?,
			status = 'live'
		WHERE id = ? AND unpublished_at IS NULL`,
		bind.InstallationID, vault, publicationID,
	)
	if err != nil {
		return fmt.Errorf("bind linear publication: %w", err)
	}
	return nil
}

// GetLinearIssueSession loads the session bound to an issue, if any.
func (r *IntegrationRepo) GetLinearIssueSession(
	ctx context.Context,
	publicationID, issueID string,
) (*LinearIssueSession, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT publication_id, issue_id, session_id, status, created_at
		FROM linear_issue_sessions
		WHERE publication_id = ? AND issue_id = ?`,
		publicationID, issueID,
	)
	var item LinearIssueSession
	err := row.Scan(
		&item.PublicationID, &item.IssueID, &item.SessionID,
		&item.Status, &item.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get linear issue session: %w", err)
	}
	return &item, nil
}

// UpsertLinearIssueSession binds an issue to a session for per_issue routing.
func (r *IntegrationRepo) UpsertLinearIssueSession(
	ctx context.Context,
	publicationID, issueID, sessionID string,
) error {
	now := time.Now().UnixMilli()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO linear_issue_sessions (
			publication_id, issue_id, session_id, status, created_at
		) VALUES (?, ?, ?, 'active', ?)
		ON CONFLICT(publication_id, issue_id) DO UPDATE SET
			session_id = excluded.session_id,
			status = 'active'`,
		publicationID, issueID, sessionID, now,
	)
	if err != nil {
		return fmt.Errorf("upsert linear issue session: %w", err)
	}
	return nil
}
