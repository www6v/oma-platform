package store

import (
	"context"
	"fmt"
)

// CountActive returns non-archived agent rows for a tenant.
func (r *AgentRepo) CountActive(
	ctx context.Context,
	tenantID string,
) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM agents
		WHERE tenant_id = ? AND archived_at IS NULL`,
		tenantOrDefault(tenantID),
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count agents: %w", err)
	}
	return n, nil
}

// CountActive returns non-archived environment rows for a tenant.
func (r *EnvironmentRepo) CountActive(
	ctx context.Context,
	tenantID string,
) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM environments
		WHERE tenant_id = ? AND archived_at IS NULL`,
		tenantOrDefault(tenantID),
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count environments: %w", err)
	}
	return n, nil
}

// CountActive returns non-archived session rows for a tenant.
func (r *SessionRepo) CountActive(
	ctx context.Context,
	tenantID string,
) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM sessions
		WHERE tenant_id = ? AND status != ?`,
		tenantOrDefault(tenantID), string(SessionStatusArchived),
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count sessions: %w", err)
	}
	return n, nil
}

// CountActive returns non-archived model card rows for a tenant.
func (r *ModelCardRepo) CountActive(
	ctx context.Context,
	tenantID string,
) (int, error) {
	var n int
	err := r.db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM model_cards
		WHERE tenant_id = ? AND archived_at IS NULL`,
		tenantOrDefault(tenantID),
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count model cards: %w", err)
	}
	return n, nil
}
