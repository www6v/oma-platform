package store

import (
	"context"
	"fmt"
)

// SessionListQuery filters a paginated session list.
type SessionListQuery struct {
	TenantID        string
	Limit           int
	Cursor          string
	Status          string
	AgentID         string
	Query           string
	CreatedAfter    *int64
	CreatedBefore   *int64
	IncludeArchived bool
}

// SessionListPage is one page of sessions.
type SessionListPage struct {
	Items      []*Session
	NextCursor string
}

// ListPage returns sessions using keyset pagination.
func (r *SessionRepo) ListPage(
	ctx context.Context,
	q SessionListQuery,
) (*SessionListPage, error) {
	tenantID := tenantOrDefault(q.TenantID)
	limit := ClampLimit(q.Limit)
	cursor, err := DecodePageCursor(q.Cursor)
	if err != nil {
		return nil, err
	}

	args := []any{tenantID}
	where := `WHERE tenant_id = ?`
	if q.Status != "" {
		where += ` AND status = ?`
		args = append(args, q.Status)
	} else if !q.IncludeArchived {
		where += ` AND status != ?`
		args = append(args, string(SessionStatusArchived))
	}
	if q.AgentID != "" {
		where += ` AND agent_id = ?`
		args = append(args, q.AgentID)
	}
	if q.Query != "" {
		like := "%" + q.Query + "%"
		where += ` AND (title LIKE ? OR id LIKE ?)`
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
		SELECT id, tenant_id, agent_id, agent_version, agent_snapshot,
			environment_id, environment_snapshot,
			title, status, turn_id, created_at, updated_at
		FROM sessions ` + where + `
		ORDER BY created_at ASC, id ASC
		LIMIT ?`
	args = append(args, fetch)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list sessions page: %w", err)
	}
	defer rows.Close()

	var items []*Session
	for rows.Next() {
		sess, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, sess)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list sessions page rows: %w", err)
	}

	page := &SessionListPage{Items: items}
	if len(items) > limit {
		page.Items = items[:limit]
		last := page.Items[len(page.Items)-1]
		next, err := EncodePageCursor(PageCursor{
			CreatedAt: last.CreatedAt,
			ID:        last.ID,
		})
		if err != nil {
			return nil, fmt.Errorf("encode session cursor: %w", err)
		}
		page.NextCursor = next
	}
	return page, nil
}
