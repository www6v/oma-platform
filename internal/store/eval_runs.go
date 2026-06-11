package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// EvalRunStatus is a run lifecycle state.
type EvalRunStatus string

const (
	EvalStatusPending   EvalRunStatus = "pending"
	EvalStatusRunning   EvalRunStatus = "running"
	EvalStatusCompleted EvalRunStatus = "completed"
	EvalStatusFailed    EvalRunStatus = "failed"
)

// EvalRunRow is one eval run record.
type EvalRunRow struct {
	ID            string
	TenantID      string
	AgentID       string
	EnvironmentID string
	Suite         sql.NullString
	Status        EvalRunStatus
	StartedAt     int64
	CompletedAt   sql.NullInt64
	Results       json.RawMessage
	Score         sql.NullFloat64
	Error         sql.NullString
}

// CreateEvalRunInput holds fields for a new eval run.
type CreateEvalRunInput struct {
	TenantID      string
	AgentID       string
	EnvironmentID string
	Suite         *string
	Status        EvalRunStatus
	Results       json.RawMessage
	Score         *float64
}

// EvalRunListOptions filters eval run listings.
type EvalRunListOptions struct {
	Limit         int
	AgentID       string
	EnvironmentID string
	Status        EvalRunStatus
}

// EvalRunRepo persists eval runs.
type EvalRunRepo struct {
	db *sql.DB
}

// NewEvalRunRepo returns a SQLite-backed eval run repository.
func NewEvalRunRepo(db *sql.DB) *EvalRunRepo {
	return &EvalRunRepo{db: db}
}

// Create inserts a new eval run.
func (r *EvalRunRepo) Create(
	ctx context.Context,
	input CreateEvalRunInput,
) (*EvalRunRow, error) {
	id := generateEvalRunID()
	tenantID := tenantOrDefault(input.TenantID)
	status := input.Status
	if status == "" {
		status = EvalStatusPending
	}
	now := time.Now().UnixMilli()
	var suite sql.NullString
	if input.Suite != nil {
		suite = sql.NullString{String: *input.Suite, Valid: true}
	}
	var score sql.NullFloat64
	if input.Score != nil {
		score = sql.NullFloat64{Float64: *input.Score, Valid: true}
	}
	results := input.Results
	if len(results) == 0 {
		results = json.RawMessage("null")
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO eval_runs (
			id, tenant_id, agent_id, environment_id, suite,
			status, started_at, results, score
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, tenantID, input.AgentID, input.EnvironmentID,
		nullSQLString(suite), string(status), now, string(results),
		nullSQLFloat(score))
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, tenantID, id)
}

// Get returns one eval run or nil.
func (r *EvalRunRepo) Get(
	ctx context.Context,
	tenantID, runID string,
) (*EvalRunRow, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, agent_id, environment_id, suite,
		       status, started_at, completed_at, results, score, error
		FROM eval_runs
		WHERE tenant_id = ? AND id = ?
	`, tenantOrDefault(tenantID), runID)
	return scanEvalRun(row)
}

// List returns eval runs matching filters.
func (r *EvalRunRepo) List(
	ctx context.Context,
	tenantID string,
	opts EvalRunListOptions,
) ([]EvalRunRow, error) {
	limit := opts.Limit
	if limit < 1 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	query := `
		SELECT id, tenant_id, agent_id, environment_id, suite,
		       status, started_at, completed_at, results, score, error
		FROM eval_runs
		WHERE tenant_id = ?
	`
	args := []any{tenantOrDefault(tenantID)}
	if opts.AgentID != "" {
		query += " AND agent_id = ?"
		args = append(args, opts.AgentID)
	}
	if opts.EnvironmentID != "" {
		query += " AND environment_id = ?"
		args = append(args, opts.EnvironmentID)
	}
	if opts.Status != "" {
		query += " AND status = ?"
		args = append(args, string(opts.Status))
	}
	query += " ORDER BY started_at DESC LIMIT ?"
	args = append(args, limit)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EvalRunRow
	for rows.Next() {
		item, err := scanEvalRunRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

// MarkFailed cancels an in-flight run before delete.
func (r *EvalRunRepo) MarkFailed(
	ctx context.Context,
	tenantID, runID, errMsg string,
) error {
	run, err := r.Get(ctx, tenantID, runID)
	if err != nil {
		return err
	}
	if run == nil {
		return ErrEvalRunNotFound
	}
	if run.Status != EvalStatusPending && run.Status != EvalStatusRunning {
		return nil
	}
	now := time.Now().UnixMilli()
	_, err = r.db.ExecContext(ctx, `
		UPDATE eval_runs
		SET status = ?, completed_at = ?, error = ?
		WHERE tenant_id = ? AND id = ?
	`, string(EvalStatusFailed), now, errMsg, tenantOrDefault(tenantID), runID)
	return err
}

// Delete removes an eval run row.
func (r *EvalRunRepo) Delete(
	ctx context.Context,
	tenantID, runID string,
) error {
	run, err := r.Get(ctx, tenantID, runID)
	if err != nil {
		return err
	}
	if run == nil {
		return ErrEvalRunNotFound
	}
	_, err = r.db.ExecContext(ctx, `
		DELETE FROM eval_runs WHERE tenant_id = ? AND id = ?
	`, tenantOrDefault(tenantID), runID)
	return err
}

// ErrEvalRunNotFound is returned when a run is missing.
var ErrEvalRunNotFound = errors.New("eval run not found")

func generateEvalRunID() string {
	return "evrun-" + randomString(idLength)
}

func nullSQLFloat(v sql.NullFloat64) any {
	if v.Valid {
		return v.Float64
	}
	return nil
}

func scanEvalRun(row *sql.Row) (*EvalRunRow, error) {
	var run EvalRunRow
	var status string
	var results sql.NullString
	err := row.Scan(
		&run.ID, &run.TenantID, &run.AgentID, &run.EnvironmentID,
		&run.Suite, &status, &run.StartedAt, &run.CompletedAt,
		&results, &run.Score, &run.Error,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	run.Status = EvalRunStatus(status)
	if results.Valid {
		run.Results = json.RawMessage(results.String)
	}
	return &run, nil
}

func scanEvalRunRows(rows *sql.Rows) (*EvalRunRow, error) {
	var run EvalRunRow
	var status string
	var results sql.NullString
	err := rows.Scan(
		&run.ID, &run.TenantID, &run.AgentID, &run.EnvironmentID,
		&run.Suite, &status, &run.StartedAt, &run.CompletedAt,
		&results, &run.Score, &run.Error,
	)
	if err != nil {
		return nil, err
	}
	run.Status = EvalRunStatus(status)
	if results.Valid {
		run.Results = json.RawMessage(results.String)
	}
	return &run, nil
}
