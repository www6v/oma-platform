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

// StoredEvent is a persisted session event row.
type StoredEvent struct {
	SessionID string
	Seq       int
	EventID   string
	Type      string
	Payload   json.RawMessage
	CreatedAt int64
}

// EventRepo appends and lists session events with monotonic seq.
type EventRepo struct {
	db      *sql.DB
	locks   sync.Map
}

// NewEventRepo returns an event log repository.
func NewEventRepo(db *sql.DB) *EventRepo {
	return &EventRepo{db: db}
}

func (r *EventRepo) lockFor(sessionID string) *sync.Mutex {
	v, _ := r.locks.LoadOrStore(sessionID, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// AppendEvents assigns sequential seq values in one transaction.
func (r *EventRepo) AppendEvents(
	ctx context.Context,
	sessionID string,
	events []json.RawMessage,
) ([]StoredEvent, error) {
	if len(events) == 0 {
		return nil, errors.New("events required")
	}

	mu := r.lockFor(sessionID)
	mu.Lock()
	defer mu.Unlock()

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var nextSeq int
	err = tx.QueryRowContext(ctx, `
		SELECT COALESCE(MAX(seq), 0) + 1 FROM session_events WHERE session_id = ?`,
		sessionID,
	).Scan(&nextSeq)
	if err != nil {
		return nil, fmt.Errorf("next seq: %w", err)
	}

	now := time.Now().UnixMilli()
	out := make([]StoredEvent, 0, len(events))
	for _, payload := range events {
		eventType, eventID, err := parseEventMeta(payload)
		if err != nil {
			return nil, err
		}
		if eventID == "" {
			eventID = generateEventID()
			payload, err = injectEventID(payload, eventID)
			if err != nil {
				return nil, err
			}
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO session_events (
				session_id, seq, event_id, type, payload, created_at
			) VALUES (?, ?, ?, ?, ?, ?)`,
			sessionID, nextSeq, eventID, eventType, string(payload), now,
		)
		if err != nil {
			return nil, fmt.Errorf("insert event: %w", err)
		}
		out = append(out, StoredEvent{
			SessionID: sessionID,
			Seq:       nextSeq,
			EventID:   eventID,
			Type:      eventType,
			Payload:   payload,
			CreatedAt: now,
		})
		nextSeq++
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit events: %w", err)
	}
	return out, nil
}

// ListEvents returns events for a session with optional pagination.
func (r *EventRepo) ListEvents(
	ctx context.Context,
	sessionID string,
	afterSeq, limit int,
	orderAsc bool,
) ([]StoredEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	query := `
		SELECT session_id, seq, event_id, type, payload, created_at
		FROM session_events
		WHERE session_id = ?`
	args := []any{sessionID}
	if afterSeq > 0 {
		if orderAsc {
			query += ` AND seq > ?`
		} else {
			query += ` AND seq < ?`
		}
		args = append(args, afterSeq)
	}
	if orderAsc {
		query += ` ORDER BY seq ASC LIMIT ?`
	} else {
		query += ` ORDER BY seq DESC LIMIT ?`
	}
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	defer rows.Close()

	var out []StoredEvent
	for rows.Next() {
		var ev StoredEvent
		var payload string
		if err := rows.Scan(
			&ev.SessionID, &ev.Seq, &ev.EventID, &ev.Type, &payload, &ev.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan event: %w", err)
		}
		ev.Payload = json.RawMessage(payload)
		out = append(out, ev)
	}
	return out, rows.Err()
}

func parseEventMeta(payload json.RawMessage) (eventType, eventID string, err error) {
	var meta struct {
		Type string `json:"type"`
		ID   string `json:"id"`
	}
	if err := json.Unmarshal(payload, &meta); err != nil {
		return "", "", fmt.Errorf("parse event payload: %w", err)
	}
	if meta.Type == "" {
		return "", "", errors.New("event type is required")
	}
	return meta.Type, meta.ID, nil
}

func injectEventID(payload json.RawMessage, eventID string) (json.RawMessage, error) {
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		return nil, err
	}
	m["id"] = eventID
	return json.Marshal(m)
}
