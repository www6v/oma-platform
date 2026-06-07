package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// SessionStatus is the lifecycle state of a session row.
type SessionStatus string

const (
	SessionStatusIdle         SessionStatus = "idle"
	SessionStatusRunning      SessionStatus = "running"
	SessionStatusInterrupted  SessionStatus = "interrupted"
	SessionStatusArchived     SessionStatus = "archived"
)

// Session is a persisted session row.
type Session struct {
	ID                  string
	TenantID            string
	AgentID             string
	AgentVersion        int
	AgentSnapshot       json.RawMessage
	EnvironmentID       string
	EnvironmentSnapshot json.RawMessage
	Title               string
	Status              SessionStatus
	TurnID              *string
	CreatedAt           int64
	UpdatedAt           *int64
}

// CreateSessionInput holds fields for session creation.
type CreateSessionInput struct {
	TenantID      string
	AgentID       string
	Title         string
	EnvironmentID string
}

// SessionRepo persists sessions in SQLite.
type SessionRepo struct {
	db        *sql.DB
	agentRepo *AgentRepo
	envRepo   *EnvironmentRepo
}

// NewSessionRepo returns a session repository.
func NewSessionRepo(
	db *sql.DB,
	agents *AgentRepo,
	envs *EnvironmentRepo,
) *SessionRepo {
	return &SessionRepo{db: db, agentRepo: agents, envRepo: envs}
}

// Create copies the current agent snapshot into a new session.
func (r *SessionRepo) Create(
	ctx context.Context,
	input CreateSessionInput,
) (*Session, error) {
	tenantID := tenantOrDefault(input.TenantID)
	agent, err := r.agentRepo.Get(ctx, tenantID, input.AgentID)
	if err != nil {
		return nil, err
	}
	if agent == nil {
		return nil, ErrNotFound
	}
	if agent.ArchivedAt != nil {
		return nil, ErrArchived
	}

	snapshot, err := json.Marshal(agent.AgentConfig)
	if err != nil {
		return nil, fmt.Errorf("marshal agent snapshot: %w", err)
	}

	envID := input.EnvironmentID
	if envID == "" {
		envID = DefaultEnvironmentID
	}
	envSnap := json.RawMessage(`{}`)
	if r.envRepo != nil {
		env, err := r.envRepo.Get(ctx, tenantID, envID)
		if err != nil {
			return nil, err
		}
		if env == nil {
			return nil, ErrNotFound
		}
		if env.ArchivedAt != nil {
			return nil, ErrArchived
		}
		envSnap, err = json.Marshal(env)
		if err != nil {
			return nil, fmt.Errorf("marshal environment snapshot: %w", err)
		}
	}

	id := generateSessionID()
	now := time.Now().UnixMilli()
	title := input.Title
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO sessions (
			id, tenant_id, agent_id, agent_version, agent_snapshot,
			environment_id, environment_snapshot,
			title, status, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, tenantID, agent.ID, agent.Version, string(snapshot),
		envID, string(envSnap),
		title, string(SessionStatusIdle), now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert session: %w", err)
	}
	return r.Get(ctx, tenantID, id)
}

// Get loads a session by tenant and id.
func (r *SessionRepo) Get(
	ctx context.Context,
	tenantID, id string,
) (*Session, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, agent_id, agent_version, agent_snapshot,
			environment_id, environment_snapshot,
			title, status, turn_id, created_at, updated_at
		FROM sessions
		WHERE id = ? AND tenant_id = ?`,
		id, tenantOrDefault(tenantID),
	)
	return scanSession(row)
}

// List returns sessions for a tenant ordered by created_at.
func (r *SessionRepo) List(
	ctx context.Context,
	tenantID string,
) ([]*Session, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, agent_id, agent_version, agent_snapshot,
			environment_id, environment_snapshot,
			title, status, turn_id, created_at, updated_at
		FROM sessions
		WHERE tenant_id = ?
		ORDER BY created_at ASC`,
		tenantOrDefault(tenantID),
	)
	if err != nil {
		return nil, fmt.Errorf("list sessions: %w", err)
	}
	defer rows.Close()

	var out []*Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// BeginTurn marks a session as running with a turn id.
func (r *SessionRepo) BeginTurn(
	ctx context.Context,
	tenantID, sessionID, turnID string,
) error {
	now := time.Now().UnixMilli()
	res, err := r.db.ExecContext(ctx, `
		UPDATE sessions
		SET status = ?, turn_id = ?, updated_at = ?
		WHERE id = ? AND tenant_id = ?`,
		string(SessionStatusRunning), turnID, now,
		sessionID, tenantOrDefault(tenantID),
	)
	if err != nil {
		return fmt.Errorf("begin turn: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// EndTurn marks a session idle and clears turn_id.
func (r *SessionRepo) EndTurn(
	ctx context.Context,
	tenantID, sessionID, turnID string,
) error {
	now := time.Now().UnixMilli()
	_, err := r.db.ExecContext(ctx, `
		UPDATE sessions
		SET status = ?, turn_id = NULL, updated_at = ?
		WHERE id = ? AND tenant_id = ? AND turn_id = ?`,
		string(SessionStatusIdle), now,
		sessionID, tenantOrDefault(tenantID), turnID,
	)
	if err != nil {
		return fmt.Errorf("end turn: %w", err)
	}
	return nil
}

// RecoverRunning marks orphan running sessions as interrupted.
func (r *SessionRepo) RecoverRunning(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		UPDATE sessions
		SET status = ?, turn_id = NULL, updated_at = ?
		WHERE status = ?`,
		string(SessionStatusInterrupted),
		time.Now().UnixMilli(),
		string(SessionStatusRunning),
	)
	if err != nil {
		return 0, fmt.Errorf("recover running sessions: %w", err)
	}
	return res.RowsAffected()
}

func scanSession(row interface {
	Scan(dest ...any) error
}) (*Session, error) {
	var (
		s            Session
		snapshot     string
		envSnapshot  string
		turnID       sql.NullString
		updatedAt    sql.NullInt64
	)
	if err := row.Scan(
		&s.ID, &s.TenantID, &s.AgentID, &s.AgentVersion, &snapshot,
		&s.EnvironmentID, &envSnapshot,
		&s.Title, &s.Status, &turnID, &s.CreatedAt, &updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan session: %w", err)
	}
	s.AgentSnapshot = json.RawMessage(snapshot)
	s.EnvironmentSnapshot = json.RawMessage(envSnapshot)
	if turnID.Valid {
		v := turnID.String
		s.TurnID = &v
	}
	if updatedAt.Valid {
		v := updatedAt.Int64
		s.UpdatedAt = &v
	}
	return &s, nil
}
