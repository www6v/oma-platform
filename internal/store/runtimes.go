package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Runtime is a registered local bridge daemon machine.
type Runtime struct {
	ID              string
	OwnerUserID     string
	OwnerTenantID   string
	MachineID       string
	Hostname        string
	OS              string
	AgentsJSON      string
	LocalSkillsJSON string
	Version         string
	Status          string
	LastHeartbeat   *int64
	CreatedAt       int64
}

// ConnectRuntimeCode is a one-time browser→CLI exchange code.
type ConnectRuntimeCode struct {
	Code      string
	UserID    string
	TenantID  string
	State     string
	ExpiresAt int64
	UsedAt    *int64
}

// RuntimeTokenAuth is the authenticated runtime identity from sk_machine_*.
type RuntimeTokenAuth struct {
	RuntimeID string
	UserID    string
	TenantID  string
}

// RuntimeTenantRow binds a runtime to a tenant API key.
type RuntimeTenantRow struct {
	TenantID      string
	AgentAPIKeyID string
}

// RuntimeTenantInfo includes display metadata for daemon /me.
type RuntimeTenantInfo struct {
	ID   string
	Name string
	Role string
}

// RuntimeRepo persists runtime registration state.
type RuntimeRepo struct {
	db *sql.DB
}

// NewRuntimeRepo returns a runtime repository.
func NewRuntimeRepo(db *sql.DB) *RuntimeRepo {
	return &RuntimeRepo{db: db}
}

// InsertConnectCode stores a one-time connect-runtime code.
func (r *RuntimeRepo) InsertConnectCode(
	ctx context.Context,
	code, userID, tenantID, state string,
	expiresAt int64,
) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO connect_runtime_codes (
			code, user_id, tenant_id, state, expires_at
		) VALUES (?, ?, ?, ?, ?)`,
		code, userID, tenantOrDefault(tenantID), state, expiresAt,
	)
	if err != nil {
		return fmt.Errorf("insert connect code: %w", err)
	}
	return nil
}

// GetConnectCode loads a connect-runtime code row.
func (r *RuntimeRepo) GetConnectCode(
	ctx context.Context,
	code string,
) (*ConnectRuntimeCode, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT code, user_id, tenant_id, state, expires_at, used_at
		FROM connect_runtime_codes WHERE code = ?`,
		code,
	)
	var item ConnectRuntimeCode
	var usedAt sql.NullInt64
	err := row.Scan(
		&item.Code, &item.UserID, &item.TenantID, &item.State,
		&item.ExpiresAt, &usedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get connect code: %w", err)
	}
	if usedAt.Valid {
		v := usedAt.Int64
		item.UsedAt = &v
	}
	return &item, nil
}

// MarkConnectCodeUsed marks a code as consumed.
func (r *RuntimeRepo) MarkConnectCodeUsed(
	ctx context.Context,
	code string,
	usedAt int64,
) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE connect_runtime_codes SET used_at = ? WHERE code = ?`,
		usedAt, code,
	)
	if err != nil {
		return fmt.Errorf("mark connect code used: %w", err)
	}
	return nil
}

// FindRuntimeByUserMachine returns an existing runtime id for idempotent setup.
func (r *RuntimeRepo) FindRuntimeByUserMachine(
	ctx context.Context,
	userID, machineID string,
) (string, bool, error) {
	var id string
	err := r.db.QueryRowContext(ctx, `
		SELECT id FROM runtimes
		WHERE owner_user_id = ? AND machine_id = ?
		LIMIT 1`,
		userID, machineID,
	).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("find runtime: %w", err)
	}
	return id, true, nil
}

// InsertRuntime creates a new runtime row.
func (r *RuntimeRepo) InsertRuntime(ctx context.Context, rt *Runtime) error {
	agentsJSON := rt.AgentsJSON
	if agentsJSON == "" {
		agentsJSON = "[]"
	}
	skillsJSON := rt.LocalSkillsJSON
	if skillsJSON == "" {
		skillsJSON = "{}"
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO runtimes (
			id, owner_user_id, owner_tenant_id, machine_id, hostname, os,
			agents_json, local_skills_json, version, status,
			last_heartbeat, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		rt.ID, rt.OwnerUserID, tenantOrDefault(rt.OwnerTenantID), rt.MachineID,
		rt.Hostname, rt.OS, agentsJSON, skillsJSON, rt.Version,
		defaultStatus(rt.Status), rt.LastHeartbeat, rt.CreatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert runtime: %w", err)
	}
	return nil
}

// UpdateRuntimeMeta refreshes hostname/os/version on re-setup.
func (r *RuntimeRepo) UpdateRuntimeMeta(
	ctx context.Context,
	id, hostname, osName, version string,
) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE runtimes SET hostname = ?, os = ?, version = ? WHERE id = ?`,
		hostname, osName, version, id,
	)
	if err != nil {
		return fmt.Errorf("update runtime meta: %w", err)
	}
	return nil
}

// InsertRuntimeToken stores a hashed sk_machine_* token.
func (r *RuntimeRepo) InsertRuntimeToken(
	ctx context.Context,
	id, runtimeID, tokenHash, userID string,
	createdAt int64,
) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO runtime_tokens (
			id, runtime_id, token_hash, created_by_user_id, created_at
		) VALUES (?, ?, ?, ?, ?)`,
		id, runtimeID, tokenHash, userID, createdAt,
	)
	if err != nil {
		return fmt.Errorf("insert runtime token: %w", err)
	}
	return nil
}

// ListByOwnerUser lists runtimes for a user newest-first.
func (r *RuntimeRepo) ListByOwnerUser(
	ctx context.Context,
	userID string,
) ([]Runtime, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, owner_user_id, owner_tenant_id, machine_id, hostname, os,
			agents_json, local_skills_json, version, status, last_heartbeat,
			created_at
		FROM runtimes
		WHERE owner_user_id = ?
		ORDER BY created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list runtimes: %w", err)
	}
	defer rows.Close()

	var out []Runtime
	for rows.Next() {
		var rt Runtime
		var hb sql.NullInt64
		if err := rows.Scan(
			&rt.ID, &rt.OwnerUserID, &rt.OwnerTenantID, &rt.MachineID,
			&rt.Hostname, &rt.OS, &rt.AgentsJSON, &rt.LocalSkillsJSON,
			&rt.Version, &rt.Status, &hb, &rt.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan runtime: %w", err)
		}
		if hb.Valid {
			v := hb.Int64
			rt.LastHeartbeat = &v
		}
		out = append(out, rt)
	}
	return out, rows.Err()
}

// GetRuntime loads a runtime by id.
func (r *RuntimeRepo) GetRuntime(
	ctx context.Context,
	id string,
) (*Runtime, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, owner_user_id, owner_tenant_id, machine_id, hostname, os,
			agents_json, local_skills_json, version, status, last_heartbeat,
			created_at
		FROM runtimes WHERE id = ?`,
		id,
	)
	var rt Runtime
	var hb sql.NullInt64
	err := row.Scan(
		&rt.ID, &rt.OwnerUserID, &rt.OwnerTenantID, &rt.MachineID,
		&rt.Hostname, &rt.OS, &rt.AgentsJSON, &rt.LocalSkillsJSON,
		&rt.Version, &rt.Status, &hb, &rt.CreatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get runtime: %w", err)
	}
	if hb.Valid {
		v := hb.Int64
		rt.LastHeartbeat = &v
	}
	return &rt, nil
}

// OwnedByUser reports whether a runtime belongs to a user.
func (r *RuntimeRepo) OwnedByUser(
	ctx context.Context,
	id, userID string,
) (bool, error) {
	var one int
	err := r.db.QueryRowContext(ctx, `
		SELECT 1 FROM runtimes WHERE id = ? AND owner_user_id = ?
		LIMIT 1`,
		id, userID,
	).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("owned runtime: %w", err)
	}
	return true, nil
}

// RevokeAndDeleteRuntime revokes tokens and deletes the runtime row.
func (r *RuntimeRepo) RevokeAndDeleteRuntime(
	ctx context.Context,
	id string,
	now int64,
) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("revoke runtime begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx, `
		UPDATE runtime_tokens SET revoked_at = ?
		WHERE runtime_id = ? AND revoked_at IS NULL`,
		now, id,
	)
	if err != nil {
		return false, fmt.Errorf("revoke runtime tokens: %w", err)
	}
	_, _ = res.RowsAffected()

	del, err := tx.ExecContext(ctx, `DELETE FROM runtimes WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("delete runtime: %w", err)
	}
	n, _ := del.RowsAffected()
	if n == 0 {
		return false, nil
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("revoke runtime commit: %w", err)
	}
	return true, nil
}

// AuthenticateToken validates a sk_machine_* hash and returns runtime identity.
func (r *RuntimeRepo) AuthenticateToken(
	ctx context.Context,
	tokenHash string,
) (*RuntimeTokenAuth, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT t.runtime_id, r.owner_user_id, r.owner_tenant_id
		FROM runtime_tokens t
		JOIN runtimes r ON r.id = t.runtime_id
		WHERE t.token_hash = ? AND t.revoked_at IS NULL`,
		tokenHash,
	)
	var auth RuntimeTokenAuth
	err := row.Scan(&auth.RuntimeID, &auth.UserID, &auth.TenantID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("authenticate runtime token: %w", err)
	}
	return &auth, nil
}

// TouchTokenLastUsed best-effort updates last_used_at.
func (r *RuntimeRepo) TouchTokenLastUsed(
	ctx context.Context,
	tokenHash string,
	now int64,
) {
	_, _ = r.db.ExecContext(ctx, `
		UPDATE runtime_tokens SET last_used_at = ? WHERE token_hash = ?`,
		now, tokenHash,
	)
}

// ListAuthorizedTenantIDs returns active runtime_tenants tenant ids.
func (r *RuntimeRepo) ListAuthorizedTenantIDs(
	ctx context.Context,
	runtimeID string,
) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT tenant_id FROM runtime_tenants
		WHERE runtime_id = ? AND revoked_at IS NULL`,
		runtimeID,
	)
	if err != nil {
		return nil, fmt.Errorf("list authorized tenants: %w", err)
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var tid string
		if err := rows.Scan(&tid); err != nil {
			return nil, fmt.Errorf("scan authorized tenant: %w", err)
		}
		out = append(out, tid)
	}
	return out, rows.Err()
}

// UpsertRuntimeTenant binds or rotates a tenant API key for a runtime.
func (r *RuntimeRepo) UpsertRuntimeTenant(
	ctx context.Context,
	runtimeID, tenantID, apiKeyID string,
	createdAt int64,
) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO runtime_tenants (
			runtime_id, tenant_id, agent_api_key_id, created_at, revoked_at
		) VALUES (?, ?, ?, ?, NULL)
		ON CONFLICT (runtime_id, tenant_id)
		DO UPDATE SET
			agent_api_key_id = excluded.agent_api_key_id,
			revoked_at = NULL`,
		runtimeID, tenantOrDefault(tenantID), apiKeyID, createdAt,
	)
	if err != nil {
		return fmt.Errorf("upsert runtime tenant: %w", err)
	}
	return nil
}

// RevokeRuntimeTenant soft-deletes a runtime tenant binding.
func (r *RuntimeRepo) RevokeRuntimeTenant(
	ctx context.Context,
	runtimeID, tenantID string,
	revokedAt int64,
) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE runtime_tenants SET revoked_at = ?
		WHERE runtime_id = ? AND tenant_id = ?`,
		revokedAt, runtimeID, tenantOrDefault(tenantID),
	)
	if err != nil {
		return fmt.Errorf("revoke runtime tenant: %w", err)
	}
	return nil
}

// ListLiveRuntimeTenants returns active runtime_tenants rows.
func (r *RuntimeRepo) ListLiveRuntimeTenants(
	ctx context.Context,
	runtimeID string,
) ([]RuntimeTenantRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT tenant_id, agent_api_key_id
		FROM runtime_tenants
		WHERE runtime_id = ? AND revoked_at IS NULL`,
		runtimeID,
	)
	if err != nil {
		return nil, fmt.Errorf("list live runtime tenants: %w", err)
	}
	defer rows.Close()

	var out []RuntimeTenantRow
	for rows.Next() {
		var row RuntimeTenantRow
		if err := rows.Scan(&row.TenantID, &row.AgentAPIKeyID); err != nil {
			return nil, fmt.Errorf("scan runtime tenant: %w", err)
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// ListRuntimeTenantsForMe returns tenant metadata for daemon /me.
func (r *RuntimeRepo) ListRuntimeTenantsForMe(
	ctx context.Context,
	runtimeID, userID string,
) ([]RuntimeTenantInfo, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT rt.tenant_id, t.name, m.role
		FROM runtime_tenants rt
		LEFT JOIN tenant t ON t.id = rt.tenant_id
		LEFT JOIN membership m
			ON m.tenant_id = rt.tenant_id AND m.user_id = ?
		WHERE rt.runtime_id = ? AND rt.revoked_at IS NULL`,
		userID, runtimeID,
	)
	if err != nil {
		return nil, fmt.Errorf("list runtime tenants for me: %w", err)
	}
	defer rows.Close()

	var out []RuntimeTenantInfo
	for rows.Next() {
		var item RuntimeTenantInfo
		var name, role sql.NullString
		if err := rows.Scan(&item.ID, &name, &role); err != nil {
			return nil, fmt.Errorf("scan runtime tenant info: %w", err)
		}
		if name.Valid && name.String != "" {
			item.Name = name.String
		} else {
			item.Name = item.ID
		}
		if role.Valid && role.String != "" {
			item.Role = role.String
		} else {
			item.Role = "member"
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// UpdateRuntimeTenantKey rotates the stored API key id for a binding.
func (r *RuntimeRepo) UpdateRuntimeTenantKey(
	ctx context.Context,
	runtimeID, tenantID, apiKeyID string,
) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE runtime_tenants SET agent_api_key_id = ?
		WHERE runtime_id = ? AND tenant_id = ?`,
		apiKeyID, runtimeID, tenantOrDefault(tenantID),
	)
	if err != nil {
		return fmt.Errorf("update runtime tenant key: %w", err)
	}
	return nil
}

func defaultStatus(status string) string {
	if status == "" {
		return "offline"
	}
	return status
}

// MarkRuntimeOnline sets status online and heartbeat timestamp.
func (r *RuntimeRepo) MarkRuntimeOnline(
	ctx context.Context,
	id string,
	now int64,
) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE runtimes
		SET status = 'online', last_heartbeat = ?
		WHERE id = ?
	`, now, id)
	if err != nil {
		return fmt.Errorf("mark runtime online: %w", err)
	}
	return nil
}

// MarkRuntimeOffline sets status offline.
func (r *RuntimeRepo) MarkRuntimeOffline(
	ctx context.Context,
	id string,
) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE runtimes SET status = 'offline' WHERE id = ?
	`, id)
	if err != nil {
		return fmt.Errorf("mark runtime offline: %w", err)
	}
	return nil
}

// TouchRuntimeHeartbeat updates last_heartbeat and online status.
func (r *RuntimeRepo) TouchRuntimeHeartbeat(
	ctx context.Context,
	id string,
	now int64,
) error {
	_, err := r.db.ExecContext(ctx, `
		UPDATE runtimes
		SET status = 'online', last_heartbeat = ?
		WHERE id = ?
	`, now, id)
	if err != nil {
		return fmt.Errorf("touch runtime heartbeat: %w", err)
	}
	return nil
}

// UpdateRuntimeHello persists daemon hello metadata.
func (r *RuntimeRepo) UpdateRuntimeHello(
	ctx context.Context,
	id, agentsJSON, version, localSkillsJSON string,
	hostname, osName *string,
	now int64,
) error {
	query := `
		UPDATE runtimes
		SET agents_json = ?, version = ?, local_skills_json = ?,
		    status = 'online', last_heartbeat = ?
	`
	args := []any{agentsJSON, version, localSkillsJSON, now}
	if hostname != nil {
		query += `, hostname = ?`
		args = append(args, *hostname)
	}
	if osName != nil {
		query += `, os = ?`
		args = append(args, *osName)
	}
	query += ` WHERE id = ?`
	args = append(args, id)
	_, err := r.db.ExecContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("update runtime hello: %w", err)
	}
	return nil
}
