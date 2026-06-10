package store

import (
	"context"
	"fmt"
)

// VaultListQuery filters a paginated vault list.
type VaultListQuery struct {
	TenantID        string
	Limit           int
	Cursor          string
	Status          string
	Query           string
	CreatedAfter    *int64
	CreatedBefore   *int64
	IncludeArchived bool
}

// VaultListPage is one page of vaults.
type VaultListPage struct {
	Items      []*Vault
	NextCursor string
}

// ListPage returns vaults using keyset pagination (newest first).
func (r *VaultRepo) ListPage(
	ctx context.Context,
	q VaultListQuery,
) (*VaultListPage, error) {
	tenantID := tenantOrDefault(q.TenantID)
	limit := ClampLimit(q.Limit)
	cursor, err := DecodePageCursor(q.Cursor)
	if err != nil {
		return nil, err
	}

	args := []any{tenantID}
	where := `WHERE tenant_id = ?`
	switch q.Status {
	case "archived":
		where += ` AND archived_at IS NOT NULL`
	case "active":
		where += ` AND archived_at IS NULL`
	case "any":
	default:
		where += ` AND archived_at IS NULL`
	}
	if q.Query != "" {
		like := "%" + q.Query + "%"
		where += ` AND name LIKE ?`
		args = append(args, like)
	}
	if q.CreatedAfter != nil {
		where += ` AND created_at >= ?`
		args = append(args, *q.CreatedAfter)
	}
	if q.CreatedBefore != nil {
		where += ` AND created_at < ?`
		args = append(args, *q.CreatedBefore)
	}
	if cursor != nil {
		where += ` AND (created_at < ? OR (created_at = ? AND id < ?))`
		args = append(args, cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
	}

	fetch := limit + 1
	query := `
		SELECT id, name, created_at, updated_at, archived_at
		FROM vaults ` + where + `
		ORDER BY created_at DESC, id DESC
		LIMIT ?`
	args = append(args, fetch)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list vaults page: %w", err)
	}
	defer rows.Close()

	var items []*Vault
	for rows.Next() {
		vault, err := scanVault(rows)
		if err != nil {
			return nil, err
		}
		vault.TenantID = tenantID
		items = append(items, vault)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list vaults page rows: %w", err)
	}

	page := &VaultListPage{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[len(page.Items)-1]
		next, err := EncodePageCursor(PageCursor{
			CreatedAt: last.CreatedAt,
			ID:        last.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("encode vault cursor: %w", err)
		}
		page.NextCursor = next
	}
	return page, nil
}
