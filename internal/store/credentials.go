package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const maxCredentialsPerVault = 20

var secretAuthFields = []string{
	"token",
	"access_token",
	"refresh_token",
	"client_secret",
}

// Credential is a vault-bound auth record.
type Credential struct {
	ID          string
	TenantID    string
	VaultID     string
	DisplayName string
	Auth        json.RawMessage
	CreatedAt   int64
	UpdatedAt   *int64
	ArchivedAt  *int64
}

// CreateCredentialInput holds fields for a new credential.
type CreateCredentialInput struct {
	TenantID    string
	VaultID     string
	DisplayName string
	Auth        json.RawMessage
}

// UpdateCredentialInput holds patch fields for Update.
type UpdateCredentialInput struct {
	DisplayName *string
	Auth        json.RawMessage
	AuthSet     bool
}

// CredentialRepo persists credentials in SQLite.
type CredentialRepo struct {
	db *sql.DB
}

// NewCredentialRepo returns a credential repository.
func NewCredentialRepo(db *sql.DB) *CredentialRepo {
	return &CredentialRepo{db: db}
}

// Create inserts a credential row.
func (r *CredentialRepo) Create(
	ctx context.Context,
	input CreateCredentialInput,
) (*Credential, error) {
	tenantID := tenantOrDefault(input.TenantID)
	if input.DisplayName == "" || len(input.Auth) == 0 {
		return nil, fmt.Errorf("display_name and auth are required")
	}
	count, err := r.countActive(ctx, tenantID, input.VaultID)
	if err != nil {
		return nil, err
	}
	if count >= maxCredentialsPerVault {
		return nil, ErrCredentialMaxExceeded
	}

	authType, mcpURL, provider, err := parseAuthMeta(input.Auth)
	if err != nil {
		return nil, err
	}
	if mcpURL != "" {
		dup, err := r.hasActiveMcpURL(ctx, tenantID, input.VaultID, mcpURL, "")
		if err != nil {
			return nil, err
		}
		if dup {
			return nil, ErrDuplicate
		}
	}

	id := generateCredentialID()
	now := time.Now().UnixMilli()
	cipher := encodeAPIKey(string(input.Auth))
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO credentials (
			id, tenant_id, vault_id, display_name, auth_type,
			mcp_server_url, provider, auth_cipher, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, tenantID, input.VaultID, input.DisplayName, authType,
		nullIfEmpty(mcpURL), nullIfEmpty(provider), cipher, now, now,
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, ErrDuplicate
		}
		return nil, fmt.Errorf("insert credential: %w", err)
	}
	return r.Get(ctx, tenantID, input.VaultID, id)
}

// Get loads one credential by id.
func (r *CredentialRepo) Get(
	ctx context.Context,
	tenantID, vaultID, id string,
) (*Credential, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, vault_id, display_name, auth_cipher,
			created_at, updated_at, archived_at
		FROM credentials
		WHERE id = ? AND tenant_id = ? AND vault_id = ?`,
		id, tenantOrDefault(tenantID), vaultID,
	)
	return scanCredential(row, tenantOrDefault(tenantID))
}

// List returns active credentials for a vault.
func (r *CredentialRepo) List(
	ctx context.Context,
	tenantID, vaultID string,
) ([]*Credential, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, vault_id, display_name, auth_cipher,
			created_at, updated_at, archived_at
		FROM credentials
		WHERE tenant_id = ? AND vault_id = ? AND archived_at IS NULL
		ORDER BY created_at ASC`,
		tenantOrDefault(tenantID), vaultID,
	)
	if err != nil {
		return nil, fmt.Errorf("list credentials: %w", err)
	}
	defer rows.Close()

	var out []*Credential
	for rows.Next() {
		cred, err := scanCredential(rows, tenantOrDefault(tenantID))
		if err != nil {
			return nil, err
		}
		out = append(out, cred)
	}
	return out, rows.Err()
}

// Update patches a credential.
func (r *CredentialRepo) Update(
	ctx context.Context,
	tenantID, vaultID, id string,
	input UpdateCredentialInput,
) (*Credential, error) {
	current, err := r.Get(ctx, tenantID, vaultID, id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrNotFound
	}
	if current.ArchivedAt != nil {
		return nil, ErrArchived
	}

	displayName := current.DisplayName
	if input.DisplayName != nil {
		displayName = *input.DisplayName
	}
	auth := current.Auth
	if input.AuthSet {
		merged, err := mergeAuthPatch(current.Auth, input.Auth)
		if err != nil {
			return nil, err
		}
		auth = merged
	}

	authType, mcpURL, provider, err := parseAuthMeta(auth)
	if err != nil {
		return nil, err
	}
	var curMap map[string]any
	if err := json.Unmarshal(current.Auth, &curMap); err == nil && curMap != nil {
		oldURL, _ := curMap["mcp_server_url"].(string)
		if oldURL != "" && mcpURL != "" && mcpURL != oldURL {
			return nil, ErrImmutableField
		}
	}

	now := time.Now().UnixMilli()
	cipher := encodeAPIKey(string(auth))
	res, err := r.db.ExecContext(ctx, `
		UPDATE credentials
		SET display_name = ?, auth_type = ?, mcp_server_url = ?,
			provider = ?, auth_cipher = ?, updated_at = ?
		WHERE id = ? AND tenant_id = ? AND vault_id = ?
			AND archived_at IS NULL`,
		displayName, authType, nullIfEmpty(mcpURL), nullIfEmpty(provider),
		cipher, now, id, tenantOrDefault(tenantID), vaultID,
	)
	if err != nil {
		return nil, fmt.Errorf("update credential: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}
	return r.Get(ctx, tenantID, vaultID, id)
}

// Archive soft-deletes a credential.
func (r *CredentialRepo) Archive(
	ctx context.Context,
	tenantID, vaultID, id string,
) (*Credential, error) {
	now := time.Now().UnixMilli()
	res, err := r.db.ExecContext(ctx, `
		UPDATE credentials
		SET archived_at = ?, updated_at = ?
		WHERE id = ? AND tenant_id = ? AND vault_id = ?
			AND archived_at IS NULL`,
		now, now, id, tenantOrDefault(tenantID), vaultID,
	)
	if err != nil {
		return nil, fmt.Errorf("archive credential: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}
	return r.Get(ctx, tenantID, vaultID, id)
}

// ArchiveByVault archives all active credentials in a vault.
func (r *CredentialRepo) ArchiveByVault(
	ctx context.Context,
	tenantID, vaultID string,
) error {
	now := time.Now().UnixMilli()
	_, err := r.db.ExecContext(ctx, `
		UPDATE credentials
		SET archived_at = ?, updated_at = ?
		WHERE tenant_id = ? AND vault_id = ? AND archived_at IS NULL`,
		now, now, tenantOrDefault(tenantID), vaultID,
	)
	return err
}

// Delete hard-deletes a credential row.
func (r *CredentialRepo) Delete(
	ctx context.Context,
	tenantID, vaultID, id string,
) error {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM credentials
		WHERE id = ? AND tenant_id = ? AND vault_id = ?`,
		id, tenantOrDefault(tenantID), vaultID,
	)
	if err != nil {
		return fmt.Errorf("delete credential: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// StripCredentialSecrets returns auth JSON with secret fields removed.
func StripCredentialSecrets(auth json.RawMessage) json.RawMessage {
	var m map[string]any
	if err := json.Unmarshal(auth, &m); err != nil {
		return auth
	}
	for _, field := range secretAuthFields {
		delete(m, field)
	}
	out, err := json.Marshal(m)
	if err != nil {
		return auth
	}
	return out
}

func (r *CredentialRepo) countActive(
	ctx context.Context,
	tenantID, vaultID string,
) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM credentials
		WHERE tenant_id = ? AND vault_id = ? AND archived_at IS NULL`,
		tenantID, vaultID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count credentials: %w", err)
	}
	return n, nil
}

func (r *CredentialRepo) hasActiveMcpURL(
	ctx context.Context,
	tenantID, vaultID, mcpURL, excludeID string,
) (bool, error) {
	args := []any{tenantID, vaultID, mcpURL}
	query := `
		SELECT COUNT(*) FROM credentials
		WHERE tenant_id = ? AND vault_id = ? AND mcp_server_url = ?
			AND archived_at IS NULL`
	if excludeID != "" {
		query += ` AND id != ?`
		args = append(args, excludeID)
	}
	var n int
	err := r.db.QueryRowContext(ctx, query, args...).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

func parseAuthMeta(auth json.RawMessage) (authType, mcpURL, provider string, err error) {
	var m map[string]any
	if err = json.Unmarshal(auth, &m); err != nil {
		return "", "", "", fmt.Errorf("invalid auth json: %w", err)
	}
	if t, ok := m["type"].(string); ok {
		authType = t
	}
	if u, ok := m["mcp_server_url"].(string); ok {
		mcpURL = u
	}
	if p, ok := m["provider"].(string); ok {
		provider = p
	}
	if authType == "" {
		return "", "", "", fmt.Errorf("auth.type is required")
	}
	return authType, mcpURL, provider, nil
}

func mergeAuthPatch(
	current json.RawMessage,
	patch json.RawMessage,
) (json.RawMessage, error) {
	var base map[string]any
	if err := json.Unmarshal(current, &base); err != nil {
		return nil, err
	}
	var delta map[string]any
	if err := json.Unmarshal(patch, &delta); err != nil {
		return nil, err
	}
	for k, v := range delta {
		base[k] = v
	}
	out, err := json.Marshal(base)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func scanCredential(row interface {
	Scan(dest ...any) error
}, tenantID string,
) (*Credential, error) {
	var (
		id          string
		vaultID     string
		displayName string
		cipher      string
		createdAt   int64
		updatedAt   sql.NullInt64
		archivedAt  sql.NullInt64
	)
	if err := row.Scan(
		&id, &vaultID, &displayName, &cipher,
		&createdAt, &updatedAt, &archivedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan credential: %w", err)
	}
	auth := json.RawMessage(decodeAPIKey(cipher))
	cred := &Credential{
		ID:          id,
		TenantID:    tenantID,
		VaultID:     vaultID,
		DisplayName: displayName,
		Auth:        auth,
		CreatedAt:   createdAt,
	}
	if updatedAt.Valid {
		v := updatedAt.Int64
		cred.UpdatedAt = &v
	}
	if archivedAt.Valid {
		v := archivedAt.Int64
		cred.ArchivedAt = &v
	}
	return cred, nil
}

func generateCredentialID() string {
	return "cred-" + randomString(idLength)
}
