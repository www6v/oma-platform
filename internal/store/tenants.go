package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
)

// TenantMembership is a user's membership in a tenant workspace.
type TenantMembership struct {
	TenantID string
	Name     string
	Role     string
}

// TenantRepo resolves multi-tenant membership for cookie sessions.
type TenantRepo struct {
	db *sql.DB
}

// NewTenantRepo returns a tenant membership store.
func NewTenantRepo(db *sql.DB) *TenantRepo {
	return &TenantRepo{db: db}
}

// DefaultTenantForUser returns the earliest membership tenant for a user.
func (r *TenantRepo) DefaultTenantForUser(
	ctx context.Context,
	userID string,
) (string, error) {
	var tenantID string
	err := r.db.QueryRowContext(ctx, `
		SELECT tenant_id FROM membership
		WHERE user_id = ?
		ORDER BY created_at ASC, tenant_id ASC
		LIMIT 1
	`, userID).Scan(&tenantID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("default tenant: %w", err)
	}
	return tenantID, nil
}

// HasMembership reports whether user belongs to tenant.
func (r *TenantRepo) HasMembership(
	ctx context.Context,
	userID string,
	tenantID string,
) (bool, error) {
	var one int
	err := r.db.QueryRowContext(ctx, `
		SELECT 1 FROM membership
		WHERE user_id = ? AND tenant_id = ?
		LIMIT 1
	`, userID, tenantID).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("has membership: %w", err)
	}
	return true, nil
}

// EnsureTenant creates a workspace for a user with no memberships.
func (r *TenantRepo) EnsureTenant(
	ctx context.Context,
	userID string,
	name string,
	email string,
) (string, error) {
	existing, err := r.DefaultTenantForUser(ctx, userID)
	if err != nil {
		return "", err
	}
	if existing != "" {
		return existing, nil
	}

	tenantID := "tn_" + randomHex(16)
	now := time.Now().UnixMilli()
	display := strings.TrimSpace(name)
	if display == "" {
		parts := strings.SplitN(strings.TrimSpace(email), "@", 2)
		display = strings.TrimSpace(parts[0])
	}
	if display == "" {
		display = "User"
	}
	tenantName := display + "'s workspace"

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("ensure tenant begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		INSERT INTO tenant (id, name, "createdAt", "updatedAt")
		VALUES (?, ?, ?, ?)
	`, tenantID, tenantName, now, now); err != nil {
		return "", fmt.Errorf("insert tenant: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO membership (user_id, tenant_id, role, created_at)
		VALUES (?, ?, 'owner', ?)
		ON CONFLICT (user_id, tenant_id) DO NOTHING
	`, userID, tenantID, now); err != nil {
		return "", fmt.Errorf("insert membership: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("ensure tenant commit: %w", err)
	}

	final, err := r.DefaultTenantForUser(ctx, userID)
	if err != nil {
		return "", err
	}
	if final != "" {
		return final, nil
	}
	return tenantID, nil
}

// ListForUser returns all tenant memberships for a user.
func (r *TenantRepo) ListForUser(
	ctx context.Context,
	userID string,
) ([]TenantMembership, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT m.tenant_id, t.name, m.role
		FROM membership m
		JOIN tenant t ON t.id = m.tenant_id
		WHERE m.user_id = ?
		ORDER BY m.created_at ASC, m.tenant_id ASC
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("list memberships: %w", err)
	}
	defer rows.Close()

	var out []TenantMembership
	for rows.Next() {
		var item TenantMembership
		if err := rows.Scan(&item.TenantID, &item.Name, &item.Role); err != nil {
			return nil, fmt.Errorf("scan membership: %w", err)
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list memberships rows: %w", err)
	}
	return out, nil
}

// GetTenantName returns the display name for a tenant id.
func (r *TenantRepo) GetTenantName(ctx context.Context, tenantID string) (string, error) {
	var name string
	err := r.db.QueryRowContext(ctx, `
		SELECT name FROM tenant WHERE id = ? LIMIT 1
	`, tenantID).Scan(&name)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get tenant name: %w", err)
	}
	return name, nil
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
