package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"
)

// PendingRow is a queued user.* event not yet promoted to session_events.
type PendingRow struct {
	SessionID       string
	PendingSeq      int
	EnqueuedAt      int64
	SessionThreadID string
	Type            string
	EventID         string
	Data            json.RawMessage
	CancelledAt     *int64
}

// PendingRepo persists the AMA-spec pending_events queue per session.
type PendingRepo struct {
	db    *sql.DB
	locks sync.Map
}

// NewPendingRepo returns a pending queue repository.
func NewPendingRepo(db *sql.DB) *PendingRepo {
	return &PendingRepo{db: db}
}

func (r *PendingRepo) lockFor(sessionID string) *sync.Mutex {
	v, _ := r.locks.LoadOrStore(sessionID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// Enqueue appends a user.* event to the pending queue.
func (r *PendingRepo) Enqueue(
	ctx context.Context,
	sessionID string,
	threadID string,
	payload json.RawMessage,
) (*PendingRow, error) {
	if threadID == "" {
		threadID = "sthr_primary"
	}
	stamped, eventID, eventType, err := StampEventPayload(payload)
	if err != nil {
		return nil, err
	}

	mu := r.lockFor(sessionID)
	mu.Lock()
	defer mu.Unlock()
	var nextSeq int
	err = r.db.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(pending_seq), 0) + 1
		FROM pending_events WHERE session_id = ?`,
		sessionID,
	).Scan(&nextSeq)
	if err != nil {
		return nil, fmt.Errorf("next pending seq: %w", err)
	}

	now := time.Now().UnixMilli()
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO pending_events (
			session_id, pending_seq, enqueued_at, session_thread_id,
			type, event_id, data
		) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		sessionID, nextSeq, now, threadID, eventType, eventID, string(stamped),
	)
	if err != nil {
		return nil, fmt.Errorf("insert pending: %w", err)
	}
	return &PendingRow{
		SessionID:       sessionID,
		PendingSeq:      nextSeq,
		EnqueuedAt:      now,
		SessionThreadID: threadID,
		Type:            eventType,
		EventID:         eventID,
		Data:            stamped,
		CancelledAt:     nil,
	}, nil
}

// Peek returns the oldest active pending row for a thread, if any.
func (r *PendingRepo) Peek(
	ctx context.Context,
	sessionID, threadID string,
) (*PendingRow, error) {
	if threadID == "" {
		threadID = "sthr_primary"
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT session_id, pending_seq, enqueued_at, session_thread_id,
		       type, event_id, data, cancelled_at
		FROM pending_events
		WHERE session_id = ? AND session_thread_id = ?
		  AND cancelled_at IS NULL
		ORDER BY pending_seq ASC LIMIT 1`,
		sessionID, threadID,
	)
	return scanPendingRow(row)
}

// Delete removes a pending row after promotion.
func (r *PendingRepo) Delete(
	ctx context.Context,
	sessionID string,
	pendingSeq int,
) error {
	_, err := r.db.ExecContext(ctx, `
		DELETE FROM pending_events
		WHERE session_id = ? AND pending_seq = ?`,
		sessionID, pendingSeq,
	)
	return err
}

// List returns pending rows for a session thread.
func (r *PendingRepo) List(
	ctx context.Context,
	sessionID, threadID string,
	includeCancelled bool,
) ([]PendingRow, error) {
	if threadID == "" {
		threadID = "sthr_primary"
	}
	query := `
		SELECT session_id, pending_seq, enqueued_at, session_thread_id,
		       type, event_id, data, cancelled_at
		FROM pending_events
		WHERE session_id = ? AND session_thread_id = ?`
	if !includeCancelled {
		query += ` AND cancelled_at IS NULL`
	}
	query += ` ORDER BY pending_seq ASC`

	rows, err := r.db.QueryContext(ctx, query, sessionID, threadID)
	if err != nil {
		return nil, fmt.Errorf("list pending: %w", err)
	}
	defer rows.Close()

	var out []PendingRow
	for rows.Next() {
		item, err := scanPendingRowRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

// CancelAllForThread marks active pending rows cancelled for a thread.
func (r *PendingRepo) CancelAllForThread(
	ctx context.Context,
	sessionID, threadID string,
	cancelledAt int64,
) ([]PendingRow, error) {
	if threadID == "" {
		threadID = "sthr_primary"
	}
	active, err := r.List(ctx, sessionID, threadID, false)
	if err != nil {
		return nil, err
	}
	if len(active) == 0 {
		return nil, nil
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE pending_events SET cancelled_at = ?
		WHERE session_id = ? AND session_thread_id = ?
		  AND cancelled_at IS NULL`,
		cancelledAt, sessionID, threadID,
	)
	if err != nil {
		return nil, fmt.Errorf("cancel pending: %w", err)
	}
	for i := range active {
		active[i].CancelledAt = &cancelledAt
	}
	return active, nil
}

// StampEventPayload ensures the payload has id and returns metadata.
func StampEventPayload(payload json.RawMessage) (
	json.RawMessage, string, string, error,
) {
	eventType, eventID, err := parseEventMeta(payload)
	if err != nil {
		return nil, "", "", err
	}
	if eventID == "" {
		eventID = generateEventID()
		payload, err = injectEventID(payload, eventID)
		if err != nil {
			return nil, "", "", err
		}
	}
	return payload, eventID, eventType, nil
}

func scanPendingRow(row *sql.Row) (*PendingRow, error) {
	var item PendingRow
	var data string
	var cancelled sql.NullInt64
	err := row.Scan(
		&item.SessionID, &item.PendingSeq, &item.EnqueuedAt,
		&item.SessionThreadID, &item.Type, &item.EventID, &data, &cancelled,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan pending: %w", err)
	}
	item.Data = json.RawMessage(data)
	if cancelled.Valid {
		v := cancelled.Int64
		item.CancelledAt = &v
	}
	return &item, nil
}

func scanPendingRowRows(rows *sql.Rows) (*PendingRow, error) {
	var item PendingRow
	var data string
	var cancelled sql.NullInt64
	err := rows.Scan(
		&item.SessionID, &item.PendingSeq, &item.EnqueuedAt,
		&item.SessionThreadID, &item.Type, &item.EventID, &data, &cancelled,
	)
	if err != nil {
		return nil, fmt.Errorf("scan pending row: %w", err)
	}
	item.Data = json.RawMessage(data)
	if cancelled.Valid {
		v := cancelled.Int64
		item.CancelledAt = &v
	}
	return &item, nil
}
