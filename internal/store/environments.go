package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const (
	// DefaultEnvironmentID is the implicit local sandbox for self-hosted MVP.
	DefaultEnvironmentID = "env-local-default"
	defaultEnvName       = "local-default"
)

// EnvironmentConfig is the API-shaped environment document.
type EnvironmentConfig struct {
	Type        string          `json:"type,omitempty"`
	ID          string          `json:"id"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Config      json.RawMessage `json:"config"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
	CreatedAt   int64           `json:"created_at"`
	UpdatedAt   *int64          `json:"updated_at,omitempty"`
	ArchivedAt  *int64          `json:"archived_at,omitempty"`
}

// CreateEnvironmentInput holds fields for a new environment.
type CreateEnvironmentInput struct {
	TenantID    string
	Name        string
	Description string
	Config      json.RawMessage
	Metadata    json.RawMessage
}

// UpdateEnvironmentInput holds patch fields for Update.
type UpdateEnvironmentInput struct {
	Name        *string
	Description *string
	Config      json.RawMessage
	ConfigSet   bool
	Metadata    json.RawMessage
	MetadataSet bool
}

// EnvironmentRepo persists environments in SQLite.
type EnvironmentRepo struct {
	db *sql.DB
}

// NewEnvironmentRepo returns an environment repository.
func NewEnvironmentRepo(db *sql.DB) *EnvironmentRepo {
	return &EnvironmentRepo{db: db}
}

// EnsureDefault creates the built-in local environment when missing.
func (r *EnvironmentRepo) EnsureDefault(ctx context.Context) error {
	tenantID := defaultTenantID
	existing, err := r.Get(ctx, tenantID, DefaultEnvironmentID)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	cfg, err := json.Marshal(map[string]string{"type": "local"})
	if err != nil {
		return err
	}
	_, err = r.Create(ctx, CreateEnvironmentInput{
		TenantID: tenantID,
		Name:     defaultEnvName,
		Config:   cfg,
	}, DefaultEnvironmentID)
	return err
}

// Create inserts a new environment row.
func (r *EnvironmentRepo) Create(
	ctx context.Context,
	input CreateEnvironmentInput,
	fixedID ...string,
) (*EnvironmentConfig, error) {
	tenantID := tenantOrDefault(input.TenantID)
	id := generateEnvironmentID()
	if len(fixedID) > 0 && fixedID[0] != "" {
		id = fixedID[0]
	}
	if len(input.Config) == 0 {
		input.Config = json.RawMessage(`{"type":"local"}`)
	}
	now := time.Now().UnixMilli()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO environments (
			id, tenant_id, name, description, status, config, metadata,
			created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, tenantID, input.Name, nullIfEmpty(input.Description),
		"ready", string(input.Config), nullableJSON(input.Metadata),
		now, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert environment: %w", err)
	}
	return r.Get(ctx, tenantID, id)
}

// Get loads one environment by id.
func (r *EnvironmentRepo) Get(
	ctx context.Context,
	tenantID, id string,
) (*EnvironmentConfig, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, name, description, config, metadata,
			created_at, updated_at, archived_at
		FROM environments
		WHERE id = ? AND tenant_id = ?`,
		id, tenantOrDefault(tenantID),
	)
	return scanEnvironment(row)
}

// List returns non-archived environments for a tenant.
func (r *EnvironmentRepo) List(
	ctx context.Context,
	tenantID string,
	includeArchived bool,
) ([]*EnvironmentConfig, error) {
	query := `
		SELECT id, name, description, config, metadata,
			created_at, updated_at, archived_at
		FROM environments
		WHERE tenant_id = ?`
	if !includeArchived {
		query += ` AND archived_at IS NULL`
	}
	query += ` ORDER BY created_at ASC`

	rows, err := r.db.QueryContext(ctx, query, tenantOrDefault(tenantID))
	if err != nil {
		return nil, fmt.Errorf("list environments: %w", err)
	}
	defer rows.Close()

	var out []*EnvironmentConfig
	for rows.Next() {
		env, err := scanEnvironment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, env)
	}
	return out, rows.Err()
}

// Update patches an environment.
func (r *EnvironmentRepo) Update(
	ctx context.Context,
	tenantID, id string,
	input UpdateEnvironmentInput,
) (*EnvironmentConfig, error) {
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

	name := current.Name
	if input.Name != nil {
		name = *input.Name
	}
	description := current.Description
	if input.Description != nil {
		description = *input.Description
	}
	config := current.Config
	if input.ConfigSet {
		config = input.Config
	}
	metadata := current.Metadata
	if input.MetadataSet {
		metadata = input.Metadata
	}
	now := time.Now().UnixMilli()
	res, err := r.db.ExecContext(ctx, `
		UPDATE environments
		SET name = ?, description = ?, config = ?, metadata = ?, updated_at = ?
		WHERE id = ? AND tenant_id = ? AND archived_at IS NULL`,
		name, nullIfEmpty(description), string(config),
		nullableJSON(metadata), now, id, tenantOrDefault(tenantID),
	)
	if err != nil {
		return nil, fmt.Errorf("update environment: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}
	return r.Get(ctx, tenantID, id)
}

// Archive soft-deletes an environment.
func (r *EnvironmentRepo) Archive(
	ctx context.Context,
	tenantID, id string,
) (*EnvironmentConfig, error) {
	if id == DefaultEnvironmentID {
		return nil, fmt.Errorf("cannot archive default environment")
	}
	now := time.Now().UnixMilli()
	res, err := r.db.ExecContext(ctx, `
		UPDATE environments
		SET archived_at = ?, updated_at = ?
		WHERE id = ? AND tenant_id = ? AND archived_at IS NULL`,
		now, now, id, tenantOrDefault(tenantID),
	)
	if err != nil {
		return nil, fmt.Errorf("archive environment: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return nil, ErrNotFound
	}
	return r.Get(ctx, tenantID, id)
}

func scanEnvironment(row interface {
	Scan(dest ...any) error
}) (*EnvironmentConfig, error) {
	var (
		id          string
		name        string
		description sql.NullString
		configJSON  string
		metadata    sql.NullString
		createdAt   int64
		updatedAt   sql.NullInt64
		archivedAt  sql.NullInt64
	)
	if err := row.Scan(
		&id, &name, &description, &configJSON, &metadata,
		&createdAt, &updatedAt, &archivedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan environment: %w", err)
	}
	env := &EnvironmentConfig{
		Type:      "environment",
		ID:        id,
		Name:      name,
		Config:    json.RawMessage(configJSON),
		CreatedAt: createdAt,
	}
	if description.Valid {
		env.Description = description.String
	}
	if metadata.Valid && metadata.String != "" {
		env.Metadata = json.RawMessage(metadata.String)
	}
	if updatedAt.Valid {
		v := updatedAt.Int64
		env.UpdatedAt = &v
	}
	if archivedAt.Valid {
		v := archivedAt.Int64
		env.ArchivedAt = &v
	}
	return env, nil
}

func generateEnvironmentID() string {
	return "env-" + randomString(idLength)
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}
