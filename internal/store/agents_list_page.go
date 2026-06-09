package store

import (
	"context"
	"fmt"
)

// AgentListQuery filters a paginated agent list.
type AgentListQuery struct {
	TenantID        string
	Limit           int
	Cursor          string
	Status          string
	Query           string
	CreatedAfter    *int64
	CreatedBefore   *int64
	IncludeArchived bool
}

// AgentListPage is one page of agents.
type AgentListPage struct {
	Items      []*Agent
	NextCursor string
}

// ListPage returns agents using keyset pagination.
func (r *AgentRepo) ListPage(
	ctx context.Context,
	q AgentListQuery,
) (*AgentListPage, error) {
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
		where += ` AND (json_extract(config, '$.name') LIKE ? OR id LIKE ?)`
		args = append(args, like, like)
	}
	if q.CreatedAfter != nil {
		where += ` AND created_at >= ?`
		args = append(args, *q.CreatedAfter)
	}
	if q.CreatedBefore != nil {
		where += ` AND created_at <= ?`
		args = append(args, *q.CreatedBefore)
	}
	if cursor != nil {
		where += ` AND (created_at > ? OR (created_at = ? AND id > ?))`
		args = append(args, cursor.CreatedAt, cursor.CreatedAt, cursor.ID)
	}

	fetch := limit + 1
	query := `
		SELECT id, config, tenant_id, created_at, updated_at, archived_at
		FROM agents ` + where + `
		ORDER BY created_at ASC, id ASC
		LIMIT ?`
	args = append(args, fetch)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list agents page: %w", err)
	}
	defer rows.Close()

	var items []*Agent
	for rows.Next() {
		agent, _, err := scanAgentWithID(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, agent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list agents page rows: %w", err)
	}

	page := &AgentListPage{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[len(page.Items)-1]
		next, err := EncodePageCursor(PageCursor{
			CreatedAt: last.CreatedAt,
			ID:        last.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("encode agent cursor: %w", err)
		}
		page.NextCursor = next
	}
	return page, nil
}
