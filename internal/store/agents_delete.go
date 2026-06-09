package store

import (
	"context"
	"fmt"
)

// HasActiveSessions reports whether the agent has non-archived sessions.
func (r *AgentRepo) HasActiveSessions(
	ctx context.Context,
	tenantID, agentID string,
) (bool, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sessions
		WHERE tenant_id = ? AND agent_id = ? AND status != ?`,
		tenantOrDefault(tenantID), agentID, string(SessionStatusArchived),
	).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("count agent sessions: %w", err)
	}
	return n > 0, nil
}

// Delete permanently removes an agent and its version history.
func (r *AgentRepo) Delete(
	ctx context.Context,
	tenantID, agentID string,
) error {
	tid := tenantOrDefault(tenantID)
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `
		DELETE FROM agent_versions WHERE agent_id = ? AND tenant_id = ?`,
		agentID, tid,
	); err != nil {
		return fmt.Errorf("delete agent versions: %w", err)
	}
	res, err := tx.ExecContext(ctx, `
		DELETE FROM agents WHERE id = ? AND tenant_id = ?`,
		agentID, tid,
	)
	if err != nil {
		return fmt.Errorf("delete agent: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete agent: %w", err)
	}
	return nil
}
