package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"time"
)

// ApiKeyMeta is the public metadata for a stored API key.
type ApiKeyMeta struct {
	ID        string
	TenantID  string
	UserID    string
	Name      string
	Prefix    string
	Source    string
	CreatedAt int64
}

// ApiKeyRepo persists tenant API keys by hash.
type ApiKeyRepo struct {
	db *sql.DB
}

// NewApiKeyRepo returns an API key repository.
func NewApiKeyRepo(db *sql.DB) *ApiKeyRepo {
	return &ApiKeyRepo{db: db}
}

// Count returns all API keys for a tenant.
func (r *ApiKeyRepo) Count(ctx context.Context, tenantID string) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM api_keys WHERE tenant_id = ?`,
		tenantOrDefault(tenantID),
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count api keys: %w", err)
	}
	return n, nil
}

// List returns key metadata for a tenant (never includes secrets).
func (r *ApiKeyRepo) List(
	ctx context.Context,
	tenantID string,
) ([]ApiKeyMeta, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, user_id, name, prefix, source, created_at
		FROM api_keys
		WHERE tenant_id = ?
		ORDER BY created_at DESC`,
		tenantOrDefault(tenantID),
	)
	if err != nil {
		return nil, fmt.Errorf("list api keys: %w", err)
	}
	defer rows.Close()

	var out []ApiKeyMeta
	for rows.Next() {
		var m ApiKeyMeta
		var userID, source sql.NullString
		if err := rows.Scan(
			&m.ID, &m.TenantID, &userID, &m.Name, &m.Prefix, &source, &m.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan api key: %w", err)
		}
		if userID.Valid {
			m.UserID = userID.String
		}
		if source.Valid {
			m.Source = source.String
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// FindByHash loads a key record by sha256 hex hash.
func (r *ApiKeyRepo) FindByHash(
	ctx context.Context,
	hash string,
) (*ApiKeyMeta, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, user_id, name, prefix, source, created_at
		FROM api_keys WHERE key_hash = ?`,
		hash,
	)
	var m ApiKeyMeta
	var userID, source sql.NullString
	err := row.Scan(
		&m.ID, &m.TenantID, &userID, &m.Name, &m.Prefix, &source, &m.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("find api key: %w", err)
	}
	if userID.Valid {
		m.UserID = userID.String
	}
	if source.Valid {
		m.Source = source.String
	}
	return &m, nil
}

// MintResult holds the one-time plaintext key and metadata.
type MintResult struct {
	ID        string
	Key       string
	Prefix    string
	Name      string
	CreatedAt int64
}

// Mint creates a new API key and returns the plaintext once.
func (r *ApiKeyRepo) Mint(
	ctx context.Context,
	tenantID, userID, name, source string,
) (*MintResult, error) {
	raw := generateRawAPIKey()
	hash := sha256Hex(raw)
	id := generateAPIKeyID()
	now := time.Now().UnixMilli()
	if name == "" {
		name = "Untitled key"
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO api_keys (
			id, tenant_id, user_id, name, key_hash, prefix, source, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, tenantOrDefault(tenantID), sqlNullString(userID), name,
		hash, raw[:8], sqlNullString(source), now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert api key: %w", err)
	}
	return &MintResult{
		ID:        id,
		Key:       raw,
		Prefix:    raw[:8],
		Name:      name,
		CreatedAt: now,
	}, nil
}

// Delete removes an API key by tenant and id.
func (r *ApiKeyRepo) Delete(
	ctx context.Context,
	tenantID, id string,
) (bool, error) {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM api_keys WHERE id = ? AND tenant_id = ?`,
		id, tenantOrDefault(tenantID),
	)
	if err != nil {
		return false, fmt.Errorf("delete api key: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func sha256Hex(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func generateRawAPIKey() string {
	const chars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	out := make([]byte, 36)
	max := big.NewInt(int64(len(chars)))
	for i := range out {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(fmt.Sprintf("api key random: %v", err))
		}
		out[i] = chars[idx.Int64()]
	}
	return "oma_" + string(out)
}

func generateAPIKeyID() string {
	return "ak_" + randomString(16)
}

func sqlNullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
