package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Vault is a tenant-scoped secret container.
type Vault struct {
	ID         string
	TenantID   string
	Name       string
	CreatedAt  int64
	UpdatedAt  *int64
	ArchivedAt *int64
}

// CreateVaultInput holds fields for a new vault.
type CreateVaultInput struct {
	TenantID string
	Name     string
}

// VaultRepo persists vault rows in SQLite.
type VaultRepo struct {
	db *sql.DB
}

// NewVaultRepo returns a vault repository.
func NewVaultRepo(db *sql.DB) *VaultRepo {
	return &VaultRepo{db: db}
}

// Create inserts a vault row.
func (r *VaultRepo) Create(
	ctx context.Context,
	input CreateVaultInput,
) (*Vault, error) {
	tenantID := tenantOrDefault(input.TenantID)
	if input.Name == "" {
		return nil, fmt.Errorf("name is required")
	}
	id := generateVaultID()
	now := time.Now().UnixMilli()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO vaults (id, tenant_id, name, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)`,
		id, tenantID, input.Name, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert vault: %w", err)
	}
	return r.Get(ctx, tenantID, id)
}

// Get loads one vault by id.
func (r *VaultRepo) Get(
	ctx context.Context,
	tenantID, id string,
) (*Vault, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, name, created_at, updated_at, archived_at
		FROM vaults
		WHERE id = ? AND tenant_id = ?`,
		id, tenantOrDefault(tenantID),
	)
	vault, err := scanVault(row)
	if err != nil {
		return nil, err
	}
	if vault == nil {
		return nil, nil
	}
	vault.TenantID = tenantOrDefault(tenantID)
	return vault, nil
}

// Exists reports whether a vault row is present.
func (r *VaultRepo) Exists(
	ctx context.Context,
	tenantID, id string,
) (bool, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM vaults
		WHERE id = ? AND tenant_id = ?`,
		id, tenantOrDefault(tenantID),
	).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("vault exists: %w", err)
	}
	return n > 0, nil
}

// Update renames a non-archived vault.
func (r *VaultRepo) Update(
	ctx context.Context,
	tenantID, id, name string,
) (*Vault, error) {
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	now := time.Now().UnixMilli()
	res, err := r.db.ExecContext(ctx, `
		UPDATE vaults
		SET name = ?, updated_at = ?
		WHERE id = ? AND tenant_id = ? AND archived_at IS NULL`,
		name, now, id, tenantOrDefault(tenantID),
	)
	if err != nil {
		return nil, fmt.Errorf("update vault: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		cur, err := r.Get(ctx, tenantID, id)
		if err != nil {
			return nil, err
		}
		if cur == nil {
			return nil, ErrNotFound
		}
		return nil, ErrArchived
	}
	return r.Get(ctx, tenantID, id)
}

// Archive soft-deletes a vault.
func (r *VaultRepo) Archive(
	ctx context.Context,
	tenantID, id string,
) (*Vault, error) {
	now := time.Now().UnixMilli()
	res, err := r.db.ExecContext(ctx, `
		UPDATE vaults
		SET archived_at = ?, updated_at = ?
		WHERE id = ? AND tenant_id = ? AND archived_at IS NULL`,
		now, now, id, tenantOrDefault(tenantID),
	)
	if err != nil {
		return nil, fmt.Errorf("archive vault: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}
	return r.Get(ctx, tenantID, id)
}

// Delete hard-deletes a vault row.
func (r *VaultRepo) Delete(
	ctx context.Context,
	tenantID, id string,
) error {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM vaults WHERE id = ? AND tenant_id = ?`,
		id, tenantOrDefault(tenantID),
	)
	if err != nil {
		return fmt.Errorf("delete vault: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// CountActive returns non-archived vault rows for a tenant.
func (r *VaultRepo) CountActive(
	ctx context.Context,
	tenantID string,
) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM vaults
		WHERE tenant_id = ? AND archived_at IS NULL`,
		tenantOrDefault(tenantID),
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count vaults: %w", err)
	}
	return n, nil
}

func scanVault(row interface {
	Scan(dest ...any) error
}) (*Vault, error) {
	var (
		id         string
		name       string
		createdAt  int64
		updatedAt  sql.NullInt64
		archivedAt sql.NullInt64
	)
	if err := row.Scan(&id, &name, &createdAt, &updatedAt, &archivedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan vault: %w", err)
	}
	v := &Vault{
		ID:        id,
		Name:      name,
		CreatedAt: createdAt,
	}
	if updatedAt.Valid {
		vv := updatedAt.Int64
		v.UpdatedAt = &vv
	}
	if archivedAt.Valid {
		vv := archivedAt.Int64
		v.ArchivedAt = &vv
	}
	return v, nil
}

func generateVaultID() string {
	return "vlt-" + randomString(idLength)
}
