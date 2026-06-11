package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// FileRow is one uploaded file metadata row.
type FileRow struct {
	ID           string
	TenantID     string
	SessionID    sql.NullString
	Scope        string
	Filename     string
	MediaType    string
	SizeBytes    int64
	Downloadable bool
	BlobKey      string
	CreatedAt    int64
}

// CreateFileInput is input for inserting a file row.
type CreateFileInput struct {
	ID           string
	TenantID     string
	SessionID    *string
	Filename     string
	MediaType    string
	SizeBytes    int64
	Downloadable bool
	BlobKey      string
}

// FileListOptions filters a tenant file listing.
type FileListOptions struct {
	SessionID *string
	BeforeID  string
	AfterID   string
	Order     string
	Limit     int
}

// FileRepo persists uploaded file metadata.
type FileRepo struct {
	db *sql.DB
}

// NewFileRepo returns a SQLite-backed file metadata repo.
func NewFileRepo(db *sql.DB) *FileRepo {
	return &FileRepo{db: db}
}

// Insert creates a new file metadata row.
func (r *FileRepo) Insert(
	ctx context.Context,
	input CreateFileInput,
) (*FileRow, error) {
	id := input.ID
	if id == "" {
		id = generateFileID()
	}
	tenantID := tenantOrDefault(input.TenantID)
	scope := "tenant"
	var sessionID sql.NullString
	if input.SessionID != nil && *input.SessionID != "" {
		scope = "session"
		sessionID = sql.NullString{
			String: *input.SessionID,
			Valid:  true,
		}
	}
	now := time.Now().UnixMilli()
	downloadable := 0
	if input.Downloadable {
		downloadable = 1
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO files (
			id, tenant_id, session_id, scope, filename, media_type,
			size_bytes, downloadable, blob_key, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, tenantID, sessionID, scope, input.Filename, input.MediaType,
		input.SizeBytes, downloadable, input.BlobKey, now,
	)
	if err != nil {
		return nil, fmt.Errorf("insert file: %w", err)
	}
	return r.Get(ctx, tenantID, id)
}

// Get loads one file row for a tenant.
func (r *FileRepo) Get(
	ctx context.Context,
	tenantID, fileID string,
) (*FileRow, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, session_id, scope, filename, media_type,
			size_bytes, downloadable, blob_key, created_at
		FROM files
		WHERE id = ? AND tenant_id = ?`,
		fileID, tenantOrDefault(tenantID),
	)
	return scanFileRow(row)
}

// List returns file rows for a tenant with optional filters.
func (r *FileRepo) List(
	ctx context.Context,
	tenantID string,
	opts FileListOptions,
) ([]FileRow, error) {
	limit := opts.Limit
	if limit < 1 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}
	order := "DESC"
	if opts.Order == "asc" {
		order = "ASC"
	}

	query := `
		SELECT id, tenant_id, session_id, scope, filename, media_type,
			size_bytes, downloadable, blob_key, created_at
		FROM files
		WHERE tenant_id = ?`
	args := []any{tenantOrDefault(tenantID)}
	if opts.SessionID != nil {
		query += ` AND session_id = ?`
		args = append(args, *opts.SessionID)
	}
	if opts.BeforeID != "" {
		query += ` AND id < ?`
		args = append(args, opts.BeforeID)
	}
	if opts.AfterID != "" {
		query += ` AND id > ?`
		args = append(args, opts.AfterID)
	}
	query += fmt.Sprintf(` ORDER BY created_at %s, id %s LIMIT ?`, order, order)
	args = append(args, limit)

	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	defer rows.Close()

	out := make([]FileRow, 0)
	for rows.Next() {
		row, err := scanFileRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *row)
	}
	return out, rows.Err()
}

// Delete removes one file row and returns the deleted row.
func (r *FileRepo) Delete(
	ctx context.Context,
	tenantID, fileID string,
) (*FileRow, error) {
	existing, err := r.Get(ctx, tenantID, fileID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, nil
	}
	_, err = r.db.ExecContext(ctx, `
		DELETE FROM files WHERE id = ? AND tenant_id = ?`,
		fileID, tenantOrDefault(tenantID),
	)
	if err != nil {
		return nil, fmt.Errorf("delete file: %w", err)
	}
	return existing, nil
}

func scanFileRow(scanner interface {
	Scan(dest ...any) error
}) (*FileRow, error) {
	var row FileRow
	var downloadable int
	err := scanner.Scan(
		&row.ID,
		&row.TenantID,
		&row.SessionID,
		&row.Scope,
		&row.Filename,
		&row.MediaType,
		&row.SizeBytes,
		&downloadable,
		&row.BlobKey,
		&row.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan file: %w", err)
	}
	row.Downloadable = downloadable == 1
	return &row, nil
}

func generateFileID() string {
	return "file-" + randomString(idLength)
}

// NewFileID returns a new unique file identifier.
func NewFileID() string {
	return generateFileID()
}
