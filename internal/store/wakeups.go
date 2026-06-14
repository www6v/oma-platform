package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

const MaxPendingWakeups = 20

// WakeupKind is one_shot or recurring cron.
type WakeupKind string

const (
	WakeupKindOneShot WakeupKind = "one_shot"
	WakeupKindCron    WakeupKind = "cron"
)

// WakeupSchedule is a persisted session self-wakeup row.
type WakeupSchedule struct {
	ID            string
	TenantID      string
	SessionID     string
	Prompt        string
	Kind          WakeupKind
	Cron          string
	FireAt        int64
	ParentEventID string
	SpanEventID   string
	ScheduledAt   string
	CreatedAt     int64
}

// WakeupRepo stores session wakeup schedules.
type WakeupRepo struct {
	db *sql.DB
}

// NewWakeupRepo returns a wakeup schedule repository.
func NewWakeupRepo(db *sql.DB) *WakeupRepo {
	return &WakeupRepo{db: db}
}

// CountPending returns pending wakeup rows for a session.
func (r *WakeupRepo) CountPending(
	ctx context.Context,
	sessionID string,
) (int, error) {
	var count int
	err := r.db.QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM session_wakeup_schedules WHERE session_id = ?`,
		sessionID,
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count wakeups: %w", err)
	}
	return count, nil
}

// Create inserts a new wakeup schedule row.
func (r *WakeupRepo) Create(
	ctx context.Context,
	row WakeupSchedule,
) error {
	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO session_wakeup_schedules (
			id, tenant_id, session_id, prompt, kind, cron,
			fire_at, parent_event_id, span_event_id,
			scheduled_at, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.ID,
		row.TenantID,
		row.SessionID,
		row.Prompt,
		string(row.Kind),
		nullString(row.Cron),
		row.FireAt,
		nullString(row.ParentEventID),
		nullString(row.SpanEventID),
		row.ScheduledAt,
		row.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("create wakeup: %w", err)
	}
	return nil
}

// Delete removes a wakeup schedule by id scoped to session.
func (r *WakeupRepo) Delete(
	ctx context.Context,
	sessionID, id string,
) (bool, error) {
	res, err := r.db.ExecContext(
		ctx,
		`DELETE FROM session_wakeup_schedules
		 WHERE session_id = ? AND id = ?`,
		sessionID, id,
	)
	if err != nil {
		return false, fmt.Errorf("delete wakeup: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListForSession returns all pending schedules for a session.
func (r *WakeupRepo) ListForSession(
	ctx context.Context,
	sessionID string,
) ([]WakeupSchedule, error) {
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, tenant_id, session_id, prompt, kind, cron,
		        fire_at, parent_event_id, span_event_id,
		        scheduled_at, created_at
		 FROM session_wakeup_schedules
		 WHERE session_id = ?
		 ORDER BY fire_at ASC`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list wakeups: %w", err)
	}
	defer rows.Close()

	var out []WakeupSchedule
	for rows.Next() {
		var row WakeupSchedule
		var cron, parentID, spanID sql.NullString
		if err := rows.Scan(
			&row.ID, &row.TenantID, &row.SessionID, &row.Prompt,
			&row.Kind, &cron, &row.FireAt, &parentID, &spanID,
			&row.ScheduledAt, &row.CreatedAt,
		); err != nil {
			return nil, err
		}
		if cron.Valid {
			row.Cron = cron.String
		}
		if parentID.Valid {
			row.ParentEventID = parentID.String
		}
		if spanID.Valid {
			row.SpanEventID = spanID.String
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListDue returns schedules with fire_at <= nowUnix.
func (r *WakeupRepo) ListDue(
	ctx context.Context,
	nowUnix int64,
	limit int,
) ([]WakeupSchedule, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := r.db.QueryContext(
		ctx,
		`SELECT id, tenant_id, session_id, prompt, kind, cron,
		        fire_at, parent_event_id, span_event_id,
		        scheduled_at, created_at
		 FROM session_wakeup_schedules
		 WHERE fire_at <= ?
		 ORDER BY fire_at ASC
		 LIMIT ?`,
		nowUnix, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list due wakeups: %w", err)
	}
	defer rows.Close()

	var out []WakeupSchedule
	for rows.Next() {
		var row WakeupSchedule
		var cron, parentID, spanID sql.NullString
		if err := rows.Scan(
			&row.ID, &row.TenantID, &row.SessionID, &row.Prompt,
			&row.Kind, &cron, &row.FireAt, &parentID, &spanID,
			&row.ScheduledAt, &row.CreatedAt,
		); err != nil {
			return nil, err
		}
		if cron.Valid {
			row.Cron = cron.String
		}
		if parentID.Valid {
			row.ParentEventID = parentID.String
		}
		if spanID.Valid {
			row.SpanEventID = spanID.String
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// UpdateFireAt sets the next fire time for a cron schedule.
func (r *WakeupRepo) UpdateFireAt(
	ctx context.Context,
	id string,
	fireAt int64,
) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE session_wakeup_schedules SET fire_at = ? WHERE id = ?`,
		fireAt, id,
	)
	if err != nil {
		return fmt.Errorf("update wakeup fire_at: %w", err)
	}
	return nil
}

func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// FireAtISO formats fire_at as RFC3339 UTC.
func (w WakeupSchedule) FireAtISO() string {
	return time.Unix(w.FireAt, 0).UTC().Format(time.RFC3339)
}
