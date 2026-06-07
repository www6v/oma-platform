package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"time"
)

const (
	defaultTenantID = "default"
	idAlphabet      = "0123456789abcdefghijklmnopqrstuvwxyz"
	idLength        = 16
)

// AgentConfig is the JSON blob stored in agents.config.
type AgentConfig struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Model        string          `json:"model"`
	SystemPrompt string          `json:"system_prompt,omitempty"`
	Description  string          `json:"description,omitempty"`
	Tools        json.RawMessage `json:"tools,omitempty"`
	Version      int             `json:"version"`
}

// AgentVersion is a historical agent snapshot row.
type AgentVersion struct {
	AgentID   string
	TenantID  string
	Version   int
	Snapshot  AgentConfig
	CreatedAt int64
}

// Agent is a persisted agent row.
type Agent struct {
	AgentConfig
	TenantID   string
	CreatedAt  int64
	UpdatedAt  *int64
	ArchivedAt *int64
}

// CreateAgentInput holds fields for a new agent.
type CreateAgentInput struct {
	TenantID     string
	Name         string
	Model        string
	SystemPrompt string
	Description  string
	Tools        json.RawMessage
}

// UpdateAgentInput holds patch fields for Update.
type UpdateAgentInput struct {
	Name         *string
	Model        *string
	SystemPrompt *string
	Description  *string
	Tools        json.RawMessage
	ToolsSet     bool
}

// AgentRepo persists agents in SQLite.
type AgentRepo struct {
	db *sql.DB
}

// NewAgentRepo returns an agent repository.
func NewAgentRepo(db *sql.DB) *AgentRepo {
	return &AgentRepo{db: db}
}

// Create inserts a new agent at version 1.
func (r *AgentRepo) Create(
	ctx context.Context,
	input CreateAgentInput,
) (*Agent, error) {
	tenantID := tenantOrDefault(input.TenantID)
	if input.Name == "" {
		return nil, errors.New("name is required")
	}
	if input.Model == "" {
		return nil, errors.New("model is required")
	}

	id := generateAgentID()
	now := time.Now().UnixMilli()
	cfg := AgentConfig{
		ID:           id,
		Name:         input.Name,
		Model:        input.Model,
		SystemPrompt: input.SystemPrompt,
		Description:  input.Description,
		Tools:        input.Tools,
		Version:      1,
	}
	configJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}

	_, err = r.db.ExecContext(ctx, `
		INSERT INTO agents (
			id, tenant_id, config, version, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?)`,
		id, tenantID, string(configJSON), cfg.Version, now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert agent: %w", err)
	}
	return r.Get(ctx, tenantID, id)
}

// Get loads an agent by tenant and id.
func (r *AgentRepo) Get(
	ctx context.Context,
	tenantID, id string,
) (*Agent, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT config, tenant_id, created_at, updated_at, archived_at
		FROM agents
		WHERE id = ? AND tenant_id = ?`,
		id, tenantOrDefault(tenantID),
	)
	return scanAgent(row)
}

// List returns agents for a tenant.
func (r *AgentRepo) List(
	ctx context.Context,
	tenantID string,
	includeArchived bool,
) ([]*Agent, error) {
	query := `
		SELECT config, tenant_id, created_at, updated_at, archived_at
		FROM agents
		WHERE tenant_id = ?`
	if !includeArchived {
		query += ` AND archived_at IS NULL`
	}
	query += ` ORDER BY created_at ASC`

	rows, err := r.db.QueryContext(ctx, query, tenantOrDefault(tenantID))
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()

	var out []*Agent
	for rows.Next() {
		agent, err := scanAgent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, agent)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list agents rows: %w", err)
	}
	return out, nil
}

// Update patches an agent and bumps its version.
func (r *AgentRepo) Update(
	ctx context.Context,
	tenantID, id string,
	input UpdateAgentInput,
) (*Agent, error) {
	tenantID = tenantOrDefault(tenantID)
	current, err := r.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrNotFound
	}
	if current.ArchivedAt != nil {
		return nil, ErrArchived
	}

	next := current.AgentConfig
	if input.Name != nil {
		next.Name = *input.Name
	}
	if input.Model != nil {
		next.Model = *input.Model
	}
	if input.SystemPrompt != nil {
		next.SystemPrompt = *input.SystemPrompt
	}
	if input.Description != nil {
		next.Description = *input.Description
	}
	if input.ToolsSet {
		next.Tools = input.Tools
	}

	if agentConfigEqual(next, current.AgentConfig) {
		return current, nil
	}

	now := time.Now().UnixMilli()
	priorSnapshot, err := json.Marshal(current.AgentConfig)
	if err != nil {
		return nil, fmt.Errorf("marshal prior snapshot: %w", err)
	}

	next.Version = current.Version + 1
	nextJSON, err := json.Marshal(next)
	if err != nil {
		return nil, fmt.Errorf("marshal next config: %w", err)
	}

	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
		INSERT INTO agent_versions (
			agent_id, tenant_id, version, snapshot, created_at
		) VALUES (?, ?, ?, ?, ?)`,
		id, tenantID, current.Version, string(priorSnapshot), now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert agent version: %w", err)
	}

	_, err = tx.ExecContext(ctx, `
		UPDATE agents
		SET config = ?, version = ?, updated_at = ?
		WHERE id = ? AND tenant_id = ?`,
		string(nextJSON), next.Version, now, id, tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("update agent: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit update: %w", err)
	}
	return r.Get(ctx, tenantID, id)
}

// Archive soft-deletes an agent.
func (r *AgentRepo) Archive(
	ctx context.Context,
	tenantID, id string,
) (*Agent, error) {
	tenantID = tenantOrDefault(tenantID)
	current, err := r.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrNotFound
	}
	if current.ArchivedAt != nil {
		return current, nil
	}

	now := time.Now().UnixMilli()
	_, err = r.db.ExecContext(ctx, `
		UPDATE agents
		SET archived_at = ?, updated_at = ?
		WHERE id = ? AND tenant_id = ?`,
		now, now, id, tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("archive agent: %w", err)
	}
	return r.Get(ctx, tenantID, id)
}

// ListVersions returns historical agent snapshots (not the current row).
func (r *AgentRepo) ListVersions(
	ctx context.Context,
	tenantID, agentID string,
) ([]AgentVersion, error) {
	tenantID = tenantOrDefault(tenantID)
	current, err := r.Get(ctx, tenantID, agentID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, ErrNotFound
	}

	rows, err := r.db.QueryContext(ctx, `
		SELECT version, snapshot, created_at
		FROM agent_versions
		WHERE agent_id = ? AND tenant_id = ?
		ORDER BY version ASC`,
		agentID, tenantID,
	)
	if err != nil {
		return nil, fmt.Errorf("list agent versions: %w", err)
	}
	defer rows.Close()

	var out []AgentVersion
	for rows.Next() {
		var (
			version   int
			snapshot  string
			createdAt int64
		)
		if err := rows.Scan(&version, &snapshot, &createdAt); err != nil {
			return nil, fmt.Errorf("scan agent version: %w", err)
		}
		var cfg AgentConfig
		if err := json.Unmarshal([]byte(snapshot), &cfg); err != nil {
			return nil, fmt.Errorf("unmarshal version snapshot: %w", err)
		}
		out = append(out, AgentVersion{
			AgentID:   agentID,
			TenantID:  tenantID,
			Version:   version,
			Snapshot:  cfg,
			CreatedAt: createdAt,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list agent versions rows: %w", err)
	}
	return out, nil
}

// GetVersion loads one historical snapshot from agent_versions.
func (r *AgentRepo) GetVersion(
	ctx context.Context,
	tenantID, agentID string,
	version int,
) (*AgentVersion, error) {
	tenantID = tenantOrDefault(tenantID)
	row := r.db.QueryRowContext(ctx, `
		SELECT version, snapshot, created_at
		FROM agent_versions
		WHERE agent_id = ? AND tenant_id = ? AND version = ?`,
		agentID, tenantID, version,
	)
	var (
		snapshot  string
		createdAt int64
	)
	if err := row.Scan(&version, &snapshot, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("get agent version: %w", err)
	}
	var cfg AgentConfig
	if err := json.Unmarshal([]byte(snapshot), &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal version snapshot: %w", err)
	}
	return &AgentVersion{
		AgentID:   agentID,
		TenantID:  tenantID,
		Version:   version,
		Snapshot:  cfg,
		CreatedAt: createdAt,
	}, nil
}

func agentConfigEqual(a, b AgentConfig) bool {
	aJSON, err := json.Marshal(a)
	if err != nil {
		return false
	}
	bJSON, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(aJSON) == string(bJSON)
}

func scanAgent(row interface {
	Scan(dest ...any) error
}) (*Agent, error) {
	var (
		configJSON string
		tenantID   string
		createdAt  int64
		updatedAt  sql.NullInt64
		archivedAt sql.NullInt64
	)
	if err := row.Scan(
		&configJSON, &tenantID, &createdAt, &updatedAt, &archivedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan agent: %w", err)
	}

	var cfg AgentConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	agent := &Agent{
		AgentConfig: cfg,
		TenantID:    tenantID,
		CreatedAt:   createdAt,
	}
	if updatedAt.Valid {
		v := updatedAt.Int64
		agent.UpdatedAt = &v
	}
	if archivedAt.Valid {
		v := archivedAt.Int64
		agent.ArchivedAt = &v
	}
	return agent, nil
}

func tenantOrDefault(tenantID string) string {
	if tenantID == "" {
		return defaultTenantID
	}
	return tenantID
}

func generateAgentID() string {
	return "agent-" + randomString(idLength)
}

func randomString(n int) string {
	out := make([]byte, n)
	max := big.NewInt(int64(len(idAlphabet)))
	for i := range out {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(fmt.Sprintf("random id: %v", err))
		}
		out[i] = idAlphabet[idx.Int64()]
	}
	return string(out)
}
