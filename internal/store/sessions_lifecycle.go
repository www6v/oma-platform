package store

import (
	"context"
	"fmt"
	"time"
)

// Archive marks a session archived and stops further event ingestion.
func (r *SessionRepo) Archive(
	ctx context.Context,
	tenantID, sessionID string,
) (*Session, error) {
	now := time.Now().UnixMilli()
	res, err := r.db.ExecContext(ctx, `
		UPDATE sessions
		SET status = ?, archived_at = ?, updated_at = ?
		WHERE id = ? AND tenant_id = ? AND status != ?`,
		string(SessionStatusArchived), now, now,
		sessionID, tenantOrDefault(tenantID), string(SessionStatusArchived),
	)
	if err != nil {
		return nil, fmt.Errorf("archive session: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		sess, err := r.Get(ctx, tenantID, sessionID)
		if err != nil {
			return nil, err
		}
		if sess == nil {
			return nil, ErrNotFound
		}
		if sess.Status == SessionStatusArchived {
			return sess, nil
		}
		return nil, ErrNotFound
	}
	return r.Get(ctx, tenantID, sessionID)
}

// Delete removes a session and its events.
func (r *SessionRepo) Delete(
	ctx context.Context,
	tenantID, sessionID string,
) error {
	tid := tenantOrDefault(tenantID)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM session_events WHERE session_id = ?`,
		sessionID,
	); err != nil {
		return fmt.Errorf("delete session events: %w", err)
	}
	res, err := tx.ExecContext(ctx, `
		DELETE FROM sessions WHERE id = ? AND tenant_id = ?`,
		sessionID, tid,
	)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete session: %w", err)
	}
	return nil
}
