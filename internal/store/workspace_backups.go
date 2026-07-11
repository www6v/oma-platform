package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

const defaultWorkspaceBackupTTL = 7 * 24 * time.Hour

// WorkspaceBackupHandle is the serialized backup_handle JSON blob.
type WorkspaceBackupHandle struct {
	ID  string `json:"id"`
	Dir string `json:"dir"`
}

// WorkspaceBackupRow is one workspace_backups metadata row.
type WorkspaceBackupRow struct {
	ID              int64
	TenantID        string
	EnvironmentID   string
	Handle          WorkspaceBackupHandle
	CreatedAt       int64
	ExpiresAt       int64
	SourceSessionID sql.NullString
}

// RecordWorkspaceBackupInput inserts a new backup row.
type RecordWorkspaceBackupInput struct {
	TenantID        string
	EnvironmentID   string
	Handle          WorkspaceBackupHandle
	SourceSessionID string
	TTL             time.Duration
}

// WorkspaceBackupRepo persists workspace snapshot metadata.
type WorkspaceBackupRepo struct {
	db *sql.DB
}

// NewWorkspaceBackupRepo returns a SQLite-backed backup registry.
func NewWorkspaceBackupRepo(db *sql.DB) *WorkspaceBackupRepo {
	return &WorkspaceBackupRepo{db: db}
}

// FindLatest returns the newest unexpired backup for a session scope.
func (r *WorkspaceBackupRepo) FindLatest(
	ctx context.Context,
	tenantID, environmentID, sessionID string,
) (*WorkspaceBackupRow, error) {
	now := time.Now().UnixMilli()
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, environment_id, backup_handle,
		       created_at, expires_at, source_session_id
		FROM workspace_backups
		WHERE tenant_id = ? AND environment_id = ?
		  AND source_session_id = ? AND expires_at > ?
		ORDER BY created_at DESC
		LIMIT 1
	`, tenantOrDefault(tenantID), environmentID, sessionID, now)
	return scanWorkspaceBackupRow(row)
}

// Record inserts a backup row and prunes older rows for the same session.
func (r *WorkspaceBackupRepo) Record(
	ctx context.Context,
	input RecordWorkspaceBackupInput,
) error {
	ttl := input.TTL
	if ttl <= 0 {
		ttl = defaultWorkspaceBackupTTL
	}
	now := time.Now().UnixMilli()
	handleJSON, err := json.Marshal(input.Handle)
	if err != nil {
		return err
	}
	envID := input.EnvironmentID
	if envID == "" {
		envID = DefaultEnvironmentID
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO workspace_backups (
			tenant_id, environment_id, backup_handle,
			created_at, expires_at, source_session_id
		) VALUES (?, ?, ?, ?, ?, ?)
	`, tenantOrDefault(input.TenantID), envID, string(handleJSON),
		now, now+ttl.Milliseconds(), nullString(input.SourceSessionID))
	if err != nil {
		return err
	}
	if input.SourceSessionID != "" {
		_, _ = r.db.ExecContext(ctx, `
			DELETE FROM workspace_backups
			WHERE tenant_id = ? AND environment_id = ?
			  AND source_session_id = ? AND created_at < ?
		`, tenantOrDefault(input.TenantID), envID,
			input.SourceSessionID, now)
	}
	return nil
}

// PruneExpired deletes rows past expires_at. Returns rows removed.
func (r *WorkspaceBackupRepo) PruneExpired(ctx context.Context) (int64, error) {
	now := time.Now().UnixMilli()
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM workspace_backups WHERE expires_at < ?
	`, now)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func scanWorkspaceBackupRow(row *sql.Row) (*WorkspaceBackupRow, error) {
	var out WorkspaceBackupRow
	var handleJSON string
	var sourceSession sql.NullString
	err := row.Scan(
		&out.ID, &out.TenantID, &out.EnvironmentID, &handleJSON,
		&out.CreatedAt, &out.ExpiresAt, &sourceSession,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(handleJSON), &out.Handle); err != nil {
		return nil, err
	}
	out.SourceSessionID = sourceSession
	return &out, nil
}
