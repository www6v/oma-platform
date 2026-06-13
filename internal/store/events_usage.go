package store

import (
	"context"
	"encoding/json"
	"fmt"
)

// UsageEventRow is one span.model_request_end event with session context.
type UsageEventRow struct {
	SessionID string
	AgentID   string
	Payload   json.RawMessage
	CreatedAt int64
}

// ListModelUsageEvents returns usage span events for a tenant since sinceMs.
func (r *EventRepo) ListModelUsageEvents(
	ctx context.Context,
	tenantID string,
	sinceMs int64,
) ([]UsageEventRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT e.session_id, s.agent_id, e.payload, e.created_at
		FROM session_events e
		INNER JOIN sessions s ON s.id = e.session_id
		WHERE s.tenant_id = ?
		  AND e.type = 'span.model_request_end'
		  AND e.created_at >= ?
		ORDER BY e.created_at ASC
	`, tenantOrDefault(tenantID), sinceMs)
	if err != nil {
		return nil, fmt.Errorf("list model usage events: %w", err)
	}
	defer rows.Close()

	var out []UsageEventRow
	for rows.Next() {
		var row UsageEventRow
		var payload string
		if err := rows.Scan(
			&row.SessionID, &row.AgentID, &payload, &row.CreatedAt,
		); err != nil {
			return nil, err
		}
		row.Payload = json.RawMessage(payload)
		out = append(out, row)
	}
	return out, rows.Err()
}
