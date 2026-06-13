package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// DreamStatus is a dream lifecycle state.
type DreamStatus string

const (
	DreamStatusPending   DreamStatus = "pending"
	DreamStatusRunning   DreamStatus = "running"
	DreamStatusCompleted DreamStatus = "completed"
	DreamStatusFailed    DreamStatus = "failed"
	DreamStatusCanceled  DreamStatus = "canceled"
)

// SupportedDreamModels lists models accepted at create time.
var SupportedDreamModels = []string{
	"claude-opus-4-7",
	"claude-sonnet-4-6",
}

const (
	maxDreamInstructionsChars = 4096
	// MaxSessionsPerDream caps session inputs on dream create.
	MaxSessionsPerDream = 100
)

// DreamUsage tracks token usage for a dream.
type DreamUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

// DreamError is a terminal failure payload.
type DreamError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// DreamRow is one dream record.
type DreamRow struct {
	ID                  string
	TenantID            string
	Status              DreamStatus
	InputMemoryStoreID  string
	InputSessionIDs     []string
	OutputMemoryStoreID sql.NullString
	Model               string
	Instructions        sql.NullString
	SessionID           sql.NullString
	Usage               DreamUsage
	Error               *DreamError
	CreatedAt           int64
	StartedAt           sql.NullInt64
	EndedAt             sql.NullInt64
	ArchivedAt          sql.NullInt64
}

// CreateDreamInput holds fields for a new dream.
type CreateDreamInput struct {
	TenantID           string
	InputMemoryStoreID string
	InputSessionIDs    []string
	Model              string
	Instructions       *string
}

// DreamListOptions filters dream listings.
type DreamListOptions struct {
	IncludeArchived bool
	Limit           int
	AfterCreatedAt  *int64
	AfterID         string
}

// DreamRepo persists dreams.
type DreamRepo struct {
	db *sql.DB
}

// NewDreamRepo returns a SQLite-backed dream repository.
func NewDreamRepo(db *sql.DB) *DreamRepo {
	return &DreamRepo{db: db}
}

// Create inserts a pending dream row.
func (r *DreamRepo) Create(
	ctx context.Context,
	input CreateDreamInput,
) (*DreamRow, error) {
	if input.InputMemoryStoreID == "" {
		return nil, errors.New("input_memory_store_id is required")
	}
	if !isSupportedDreamModel(input.Model) {
		return nil, fmt.Errorf(
			"model must be one of: %s",
			joinDreamModels(),
		)
	}
	if input.Instructions != nil &&
		len(*input.Instructions) > maxDreamInstructionsChars {
		return nil, fmt.Errorf(
			"instructions exceeds %d character limit",
			maxDreamInstructionsChars,
		)
	}
	sessionIDs := dedupeSessionIDs(input.InputSessionIDs)
	if len(sessionIDs) > MaxSessionsPerDream {
		return nil, fmt.Errorf(
			"sessions per dream capped at %d",
			MaxSessionsPerDream,
		)
	}
	id := generateDreamID()
	tenantID := tenantOrDefault(input.TenantID)
	now := time.Now().UnixMilli()
	sessionJSON, err := json.Marshal(sessionIDs)
	if err != nil {
		return nil, err
	}
	usageJSON, err := json.Marshal(zeroDreamUsage())
	if err != nil {
		return nil, err
	}
	var instructions sql.NullString
	if input.Instructions != nil {
		instructions = sql.NullString{
			String: *input.Instructions,
			Valid:  true,
		}
	}
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO dreams (
			id, tenant_id, status, input_memory_store_id, input_session_ids,
			model, instructions, usage, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, tenantID, string(DreamStatusPending), input.InputMemoryStoreID,
		string(sessionJSON), input.Model, nullSQLString(instructions),
		string(usageJSON), now)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, tenantID, id)
}

// Get returns one dream or nil.
func (r *DreamRepo) Get(
	ctx context.Context,
	tenantID, dreamID string,
) (*DreamRow, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, status, input_memory_store_id, input_session_ids,
		       output_memory_store_id, model, instructions, session_id, usage,
		       error, created_at, started_at, ended_at, archived_at
		FROM dreams
		WHERE tenant_id = ? AND id = ?
	`, tenantOrDefault(tenantID), dreamID)
	return scanDreamRow(row)
}

// List returns dreams for a tenant.
func (r *DreamRepo) List(
	ctx context.Context,
	tenantID string,
	opts DreamListOptions,
) ([]DreamRow, bool, error) {
	limit := opts.Limit
	if limit < 1 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	fetch := limit + 1
	query := `
		SELECT id, tenant_id, status, input_memory_store_id, input_session_ids,
		       output_memory_store_id, model, instructions, session_id, usage,
		       error, created_at, started_at, ended_at, archived_at
		FROM dreams
		WHERE tenant_id = ?
	`
	args := []any{tenantOrDefault(tenantID)}
	if !opts.IncludeArchived {
		query += " AND archived_at IS NULL"
	}
	if opts.AfterCreatedAt != nil && opts.AfterID != "" {
		query += " AND (created_at < ? OR (created_at = ? AND id < ?))"
		args = append(args, *opts.AfterCreatedAt, *opts.AfterCreatedAt, opts.AfterID)
	}
	query += " ORDER BY created_at DESC, id DESC LIMIT ?"
	args = append(args, fetch)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	var out []DreamRow
	for rows.Next() {
		item, err := scanDreamRows(rows)
		if err != nil {
			return nil, false, err
		}
		out = append(out, *item)
	}
	if err := rows.Err(); err != nil {
		return nil, false, err
	}
	hasMore := len(out) > limit
	if hasMore {
		out = out[:limit]
	}
	return out, hasMore, nil
}

// ListActive returns pending and running dreams.
func (r *DreamRepo) ListActive(ctx context.Context) ([]DreamRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, status, input_memory_store_id, input_session_ids,
		       output_memory_store_id, model, instructions, session_id, usage,
		       error, created_at, started_at, ended_at, archived_at
		FROM dreams
		WHERE status IN (?, ?)
		ORDER BY created_at ASC
	`, string(DreamStatusPending), string(DreamStatusRunning))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectDreamRows(rows)
}

// FindActiveByOutputStore returns non-terminal dreams bound to a store.
func (r *DreamRepo) FindActiveByOutputStore(
	ctx context.Context,
	tenantID, storeID string,
) ([]DreamRow, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, tenant_id, status, input_memory_store_id, input_session_ids,
		       output_memory_store_id, model, instructions, session_id, usage,
		       error, created_at, started_at, ended_at, archived_at
		FROM dreams
		WHERE tenant_id = ? AND output_memory_store_id = ?
		  AND status IN (?, ?)
	`, tenantOrDefault(tenantID), storeID,
		string(DreamStatusPending), string(DreamStatusRunning))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return collectDreamRows(rows)
}

// MarkRunning transitions pending → running.
func (r *DreamRepo) MarkRunning(
	ctx context.Context,
	tenantID, dreamID, outputStoreID string,
	sessionID *string,
) error {
	now := time.Now().UnixMilli()
	var session sql.NullString
	if sessionID != nil && *sessionID != "" {
		session = sql.NullString{String: *sessionID, Valid: true}
	}
	res, err := r.db.ExecContext(ctx, `
		UPDATE dreams
		SET status = ?, output_memory_store_id = ?, session_id = ?,
		    started_at = ?
		WHERE tenant_id = ? AND id = ? AND status = ?
	`, string(DreamStatusRunning), outputStoreID, nullSQLString(session),
		now, tenantOrDefault(tenantID), dreamID, string(DreamStatusPending))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrDreamInvalidState
	}
	return nil
}

// MarkCompleted transitions running → completed.
func (r *DreamRepo) MarkCompleted(
	ctx context.Context,
	tenantID, dreamID string,
) error {
	now := time.Now().UnixMilli()
	res, err := r.db.ExecContext(ctx, `
		UPDATE dreams
		SET status = ?, ended_at = ?
		WHERE tenant_id = ? AND id = ? AND status = ?
	`, string(DreamStatusCompleted), now,
		tenantOrDefault(tenantID), dreamID, string(DreamStatusRunning))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrDreamInvalidState
	}
	return nil
}

// MarkFailed sets terminal failed status with error JSON.
func (r *DreamRepo) MarkFailed(
	ctx context.Context,
	tenantID, dreamID string,
	dreamErr DreamError,
) error {
	now := time.Now().UnixMilli()
	payload, err := json.Marshal(dreamErr)
	if err != nil {
		return err
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE dreams
		SET status = ?, error = ?, ended_at = ?
		WHERE tenant_id = ? AND id = ?
		  AND status IN (?, ?)
	`, string(DreamStatusFailed), string(payload), now,
		tenantOrDefault(tenantID), dreamID,
		string(DreamStatusPending), string(DreamStatusRunning))
	return err
}

// Cancel transitions a non-terminal dream to canceled.
func (r *DreamRepo) Cancel(
	ctx context.Context,
	tenantID, dreamID string,
) (*DreamRow, error) {
	dream, err := r.Get(ctx, tenantID, dreamID)
	if err != nil {
		return nil, err
	}
	if dream == nil {
		return nil, ErrDreamNotFound
	}
	if dream.Status == DreamStatusCanceled {
		return dream, nil
	}
	if isDreamTerminal(dream.Status) {
		return nil, ErrDreamInvalidState
	}
	now := time.Now().UnixMilli()
	_, err = r.db.ExecContext(ctx, `
		UPDATE dreams
		SET status = ?, ended_at = ?
		WHERE tenant_id = ? AND id = ?
	`, string(DreamStatusCanceled), now,
		tenantOrDefault(tenantID), dreamID)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, tenantID, dreamID)
}

// Archive sets archived_at on a terminal dream.
func (r *DreamRepo) Archive(
	ctx context.Context,
	tenantID, dreamID string,
) (*DreamRow, error) {
	dream, err := r.Get(ctx, tenantID, dreamID)
	if err != nil {
		return nil, err
	}
	if dream == nil {
		return nil, ErrDreamNotFound
	}
	if dream.ArchivedAt.Valid {
		return dream, nil
	}
	if !isDreamTerminal(dream.Status) {
		return nil, ErrDreamInvalidState
	}
	now := time.Now().UnixMilli()
	_, err = r.db.ExecContext(ctx, `
		UPDATE dreams SET archived_at = ?
		WHERE tenant_id = ? AND id = ?
	`, now, tenantOrDefault(tenantID), dreamID)
	if err != nil {
		return nil, err
	}
	return r.Get(ctx, tenantID, dreamID)
}

// ErrDreamNotFound is returned when a dream is missing.
var ErrDreamNotFound = errors.New("dream not found")

// ErrDreamInvalidState is returned for illegal transitions.
var ErrDreamInvalidState = errors.New("dream invalid state")

func generateDreamID() string {
	return "drm-" + randomString(idLength)
}

func zeroDreamUsage() DreamUsage {
	return DreamUsage{}
}

func isDreamTerminal(status DreamStatus) bool {
	switch status {
	case DreamStatusCompleted, DreamStatusFailed, DreamStatusCanceled:
		return true
	default:
		return false
	}
}

func isSupportedDreamModel(model string) bool {
	for _, m := range SupportedDreamModels {
		if m == model {
			return true
		}
	}
	return false
}

func joinDreamModels() string {
	out := ""
	for i, m := range SupportedDreamModels {
		if i > 0 {
			out += ", "
		}
		out += m
	}
	return out
}

func dedupeSessionIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func collectDreamRows(rows *sql.Rows) ([]DreamRow, error) {
	var out []DreamRow
	for rows.Next() {
		item, err := scanDreamRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

func scanDreamRow(row *sql.Row) (*DreamRow, error) {
	var dream DreamRow
	var status string
	var sessionIDsJSON string
	var usageJSON string
	var errJSON sql.NullString
	err := row.Scan(
		&dream.ID, &dream.TenantID, &status,
		&dream.InputMemoryStoreID, &sessionIDsJSON,
		&dream.OutputMemoryStoreID, &dream.Model,
		&dream.Instructions, &dream.SessionID, &usageJSON,
		&errJSON, &dream.CreatedAt, &dream.StartedAt,
		&dream.EndedAt, &dream.ArchivedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	dream.Status = DreamStatus(status)
	_ = json.Unmarshal([]byte(sessionIDsJSON), &dream.InputSessionIDs)
	if dream.InputSessionIDs == nil {
		dream.InputSessionIDs = []string{}
	}
	_ = json.Unmarshal([]byte(usageJSON), &dream.Usage)
	if errJSON.Valid && errJSON.String != "" {
		var dreamErr DreamError
		if json.Unmarshal([]byte(errJSON.String), &dreamErr) == nil {
			dream.Error = &dreamErr
		}
	}
	return &dream, nil
}

func scanDreamRows(rows *sql.Rows) (*DreamRow, error) {
	var dream DreamRow
	var status string
	var sessionIDsJSON string
	var usageJSON string
	var errJSON sql.NullString
	err := rows.Scan(
		&dream.ID, &dream.TenantID, &status,
		&dream.InputMemoryStoreID, &sessionIDsJSON,
		&dream.OutputMemoryStoreID, &dream.Model,
		&dream.Instructions, &dream.SessionID, &usageJSON,
		&errJSON, &dream.CreatedAt, &dream.StartedAt,
		&dream.EndedAt, &dream.ArchivedAt,
	)
	if err != nil {
		return nil, err
	}
	dream.Status = DreamStatus(status)
	_ = json.Unmarshal([]byte(sessionIDsJSON), &dream.InputSessionIDs)
	if dream.InputSessionIDs == nil {
		dream.InputSessionIDs = []string{}
	}
	_ = json.Unmarshal([]byte(usageJSON), &dream.Usage)
	if errJSON.Valid && errJSON.String != "" {
		var dreamErr DreamError
		if json.Unmarshal([]byte(errJSON.String), &dreamErr) == nil {
			dream.Error = &dreamErr
		}
	}
	return &dream, nil
}
