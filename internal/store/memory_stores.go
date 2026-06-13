package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	maxMemoryInlineBytes = 4 * 1024
	maxMemoryInlineOnlyBytes = 100 * 1024
	maxMemoryBlobBytes = 10 * 1024 * 1024
	maxMemoryVersionInlineBytes = 100 * 1024
)

// MemoryBlobStore persists large memory payloads outside SQLite.
type MemoryBlobStore interface {
	Write(tenantID, storeID, memoryID string, data []byte) (key string, err error)
	Read(key string) ([]byte, error)
	Delete(key string) error
}

// MemoryStoreRow is one memory store record.
type MemoryStoreRow struct {
	ID          string
	TenantID    string
	Name        string
	Description sql.NullString
	CreatedAt   int64
	UpdatedAt   sql.NullInt64
	ArchivedAt  sql.NullInt64
}

// MemoryRow is one memory entry (small content inline, large content in blob).
type MemoryRow struct {
	ID            string
	StoreID       string
	Path          string
	Content       string
	BlobKey       string
	ContentSHA256 string
	ETag          string
	SizeBytes     int64
	CreatedAt     int64
	UpdatedAt     int64
}

// MemoryVersionRow is an append-only audit entry.
type MemoryVersionRow struct {
	ID            string
	MemoryID      string
	StoreID       string
	Operation     string
	Path          sql.NullString
	Content       sql.NullString
	ContentSHA256 sql.NullString
	SizeBytes     sql.NullInt64
	ActorType     string
	ActorID       string
	CreatedAt     int64
	Redacted      bool
}

// MemoryStoreListOptions filters store listings.
type MemoryStoreListOptions struct {
	Status        string
	IncludeArchived bool
	CreatedAfter  *int64
	CreatedBefore *int64
}

// MemoryStoreRepo persists memory stores, memories, and versions.
type MemoryStoreRepo struct {
	db    *sql.DB
	blobs MemoryBlobStore
}

// NewMemoryStoreRepo returns a SQLite-backed memory repository.
func NewMemoryStoreRepo(db *sql.DB, blobs MemoryBlobStore) *MemoryStoreRepo {
	return &MemoryStoreRepo{db: db, blobs: blobs}
}

// CreateStore inserts a new memory store.
func (r *MemoryStoreRepo) CreateStore(
	ctx context.Context,
	tenantID, name string,
	description *string,
) (*MemoryStoreRow, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, errors.New("name is required")
	}
	id := generateMemoryStoreID()
	tenantID = tenantOrDefault(tenantID)
	now := time.Now().UnixMilli()
	var desc sql.NullString
	if description != nil && *description != "" {
		desc = sql.NullString{String: *description, Valid: true}
	}
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO memory_stores (
			id, tenant_id, name, description, created_at
		) VALUES (?, ?, ?, ?, ?)
	`, id, tenantID, name, nullSQLString(desc), now)
	if err != nil {
		return nil, err
	}
	return r.GetStore(ctx, tenantID, id)
}

// GetStore returns one store or nil.
func (r *MemoryStoreRepo) GetStore(
	ctx context.Context,
	tenantID, storeID string,
) (*MemoryStoreRow, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, tenant_id, name, description, created_at, updated_at, archived_at
		FROM memory_stores
		WHERE tenant_id = ? AND id = ?
	`, tenantOrDefault(tenantID), storeID)
	return scanMemoryStore(row)
}

// ListStores returns stores matching filters.
func (r *MemoryStoreRepo) ListStores(
	ctx context.Context,
	tenantID string,
	opts MemoryStoreListOptions,
) ([]MemoryStoreRow, error) {
	status := opts.Status
	if status == "" {
		if opts.IncludeArchived {
			status = "any"
		} else {
			status = "active"
		}
	}
	query := `
		SELECT id, tenant_id, name, description, created_at, updated_at, archived_at
		FROM memory_stores
		WHERE tenant_id = ?
	`
	args := []any{tenantOrDefault(tenantID)}
	switch status {
	case "active":
		query += " AND archived_at IS NULL"
	case "archived":
		query += " AND archived_at IS NOT NULL"
	case "any":
	default:
		return nil, fmt.Errorf("invalid status %q", status)
	}
	if opts.CreatedAfter != nil {
		query += " AND created_at >= ?"
		args = append(args, *opts.CreatedAfter)
	}
	if opts.CreatedBefore != nil {
		query += " AND created_at < ?"
		args = append(args, *opts.CreatedBefore)
	}
	query += " ORDER BY created_at DESC"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MemoryStoreRow
	for rows.Next() {
		item, err := scanMemoryStoreRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

// UpdateStore patches name and/or description.
func (r *MemoryStoreRepo) UpdateStore(
	ctx context.Context,
	tenantID, storeID string,
	name *string,
	description **string,
) (*MemoryStoreRow, error) {
	store, err := r.GetStore(ctx, tenantID, storeID)
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, ErrMemoryStoreNotFound
	}
	now := time.Now().UnixMilli()
	newName := store.Name
	if name != nil {
		newName = strings.TrimSpace(*name)
		if newName == "" {
			return nil, errors.New("name is required")
		}
	}
	var desc sql.NullString
	if description != nil {
		if *description == nil {
			desc = sql.NullString{}
		} else {
			desc = sql.NullString{String: **description, Valid: true}
		}
	} else {
		desc = store.Description
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE memory_stores
		SET name = ?, description = ?, updated_at = ?
		WHERE tenant_id = ? AND id = ?
	`, newName, nullSQLString(desc), now, tenantOrDefault(tenantID), storeID)
	if err != nil {
		return nil, err
	}
	return r.GetStore(ctx, tenantID, storeID)
}

// ArchiveStore sets archived_at.
func (r *MemoryStoreRepo) ArchiveStore(
	ctx context.Context,
	tenantID, storeID string,
) (*MemoryStoreRow, error) {
	store, err := r.GetStore(ctx, tenantID, storeID)
	if err != nil {
		return nil, err
	}
	if store == nil {
		return nil, ErrMemoryStoreNotFound
	}
	now := time.Now().UnixMilli()
	_, err = r.db.ExecContext(ctx, `
		UPDATE memory_stores
		SET archived_at = ?, updated_at = ?
		WHERE tenant_id = ? AND id = ?
	`, now, now, tenantOrDefault(tenantID), storeID)
	if err != nil {
		return nil, err
	}
	return r.GetStore(ctx, tenantID, storeID)
}

// DeleteStore removes a store and cascaded memories/versions.
func (r *MemoryStoreRepo) DeleteStore(
	ctx context.Context,
	tenantID, storeID string,
) error {
	store, err := r.GetStore(ctx, tenantID, storeID)
	if err != nil {
		return err
	}
	if store == nil {
		return ErrMemoryStoreNotFound
	}
	if r.blobs != nil {
		keys, err := r.ListMemoryBlobKeys(ctx, storeID)
		if err != nil {
			return err
		}
		for _, key := range keys {
			_ = r.blobs.Delete(key)
		}
	}
	_, err = r.db.ExecContext(ctx, `
		DELETE FROM memory_stores WHERE tenant_id = ? AND id = ?
	`, tenantOrDefault(tenantID), storeID)
	return err
}

// WriteMemory creates or replaces a memory at path.
func (r *MemoryStoreRepo) WriteMemory(
	ctx context.Context,
	tenantID, storeID, path, content, actorType, actorID string,
	precondition map[string]string,
) (*MemoryRow, error) {
	if err := r.requireStore(ctx, tenantID, storeID); err != nil {
		return nil, err
	}
	if path == "" {
		return nil, errors.New("path is required")
	}
	if int64(len(content)) > maxAllowedMemoryBytes(r.blobs != nil) {
		return nil, ErrMemoryContentTooLarge
	}
	existing, err := r.getMemoryByPath(ctx, storeID, path)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		if pre, ok := precondition["if_absent"]; ok && pre != "true" {
			return nil, ErrMemoryPreconditionFailed
		}
		return r.insertMemory(
			ctx, tenantID, storeID, path, content, actorType, actorID, "create",
		)
	}
	if pre, ok := precondition["content_sha256"]; ok && pre != existing.ContentSHA256 {
		return nil, ErrMemoryPreconditionFailed
	}
	return r.updateMemoryContent(
		ctx, tenantID, existing, content, actorType, actorID, "update",
	)
}

// ListMemories returns memories for a store (metadata includes content for MVP).
func (r *MemoryStoreRepo) ListMemories(
	ctx context.Context,
	tenantID, storeID, pathPrefix string,
) ([]MemoryRow, error) {
	if err := r.requireStore(ctx, tenantID, storeID); err != nil {
		return nil, err
	}
	query := memorySelectSQL + `
		WHERE store_id = ?
	`
	args := []any{storeID}
	if pathPrefix != "" {
		query += " AND path LIKE ?"
		args = append(args, pathPrefix+"%")
	}
	query += " ORDER BY updated_at DESC"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MemoryRow
	for rows.Next() {
		item, err := scanMemoryRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

// GetMemory returns one memory by id.
func (r *MemoryStoreRepo) GetMemory(
	ctx context.Context,
	tenantID, storeID, memoryID string,
) (*MemoryRow, error) {
	if err := r.requireStore(ctx, tenantID, storeID); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT id, store_id, path, content, blob_key, content_sha256, etag,
		       size_bytes, created_at, updated_at
		FROM memories
		WHERE store_id = ? AND id = ?
	`, storeID, memoryID)
	mem, err := scanMemory(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err := r.hydrateMemory(mem); err != nil {
		return nil, err
	}
	return mem, nil
}

// UpdateMemory updates path and/or content by id.
func (r *MemoryStoreRepo) UpdateMemory(
	ctx context.Context,
	tenantID, storeID, memoryID string,
	path, content *string,
	actorType, actorID string,
	precondition map[string]string,
) (*MemoryRow, error) {
	if err := r.requireStore(ctx, tenantID, storeID); err != nil {
		return nil, err
	}
	existing, err := r.GetMemory(ctx, tenantID, storeID, memoryID)
	if err != nil {
		return nil, err
	}
	if existing == nil {
		return nil, ErrMemoryNotFound
	}
	if pre, ok := precondition["content_sha256"]; ok && pre != existing.ContentSHA256 {
		return nil, ErrMemoryPreconditionFailed
	}
	newPath := existing.Path
	if path != nil {
		newPath = *path
	}
	newContent := existing.Content
	if content != nil {
		newContent = *content
	}
	if int64(len(newContent)) > maxAllowedMemoryBytes(r.blobs != nil) {
		return nil, ErrMemoryContentTooLarge
	}
	if newPath != existing.Path {
		other, err := r.getMemoryByPath(ctx, storeID, newPath)
		if err != nil {
			return nil, err
		}
		if other != nil && other.ID != memoryID {
			return nil, ErrMemoryPreconditionFailed
		}
	}
	inline, blobKey, err := r.persistContent(
		tenantID, storeID, memoryID, newContent, existing.BlobKey,
	)
	if err != nil {
		return nil, err
	}
	sha := hashContent(newContent)
	etag := sha
	now := time.Now().UnixMilli()
	size := int64(len(newContent))
	_, err = r.db.ExecContext(ctx, `
		UPDATE memories
		SET path = ?, content = ?, blob_key = ?, content_sha256 = ?, etag = ?,
		    size_bytes = ?, updated_at = ?
		WHERE id = ? AND store_id = ?
	`, newPath, inline, nullEmptyString(blobKey), sha, etag, size, now,
		memoryID, storeID)
	if err != nil {
		return nil, err
	}
	versionContent := versionSnapshotContent(newContent)
	if err := r.insertVersion(ctx, memoryVersionInput{
		MemoryID:      memoryID,
		StoreID:       storeID,
		Operation:     "update",
		Path:          &newPath,
		Content:       versionContent,
		ContentSHA256: &sha,
		SizeBytes:     &size,
		ActorType:     actorType,
		ActorID:       actorID,
	}); err != nil {
		return nil, err
	}
	return r.GetMemory(ctx, tenantID, storeID, memoryID)
}

// DeleteMemory removes a memory by id.
func (r *MemoryStoreRepo) DeleteMemory(
	ctx context.Context,
	tenantID, storeID, memoryID, expectedSHA, actorType, actorID string,
) error {
	if err := r.requireStore(ctx, tenantID, storeID); err != nil {
		return err
	}
	existing, err := r.GetMemory(ctx, tenantID, storeID, memoryID)
	if err != nil {
		return err
	}
	if existing == nil {
		return ErrMemoryNotFound
	}
	if expectedSHA != "" && existing.ContentSHA256 != expectedSHA {
		return ErrMemoryPreconditionFailed
	}
	path := existing.Path
	size := existing.SizeBytes
	sha := existing.ContentSHA256
	if err := r.insertVersion(ctx, memoryVersionInput{
		MemoryID:      memoryID,
		StoreID:       storeID,
		Operation:     "delete",
		Path:          &path,
		ContentSHA256: &sha,
		SizeBytes:     &size,
		ActorType:     actorType,
		ActorID:       actorID,
	}); err != nil {
		return err
	}
	if existing.BlobKey != "" && r.blobs != nil {
		_ = r.blobs.Delete(existing.BlobKey)
	}
	_, err = r.db.ExecContext(ctx, `
		DELETE FROM memories WHERE store_id = ? AND id = ?
	`, storeID, memoryID)
	return err
}

// ListVersions returns version rows for a store.
func (r *MemoryStoreRepo) ListVersions(
	ctx context.Context,
	tenantID, storeID, memoryID string,
) ([]MemoryVersionRow, error) {
	if err := r.requireStore(ctx, tenantID, storeID); err != nil {
		return nil, err
	}
	query := `
		SELECT id, memory_id, store_id, operation, path, content,
		       content_sha256, size_bytes, actor_type, actor_id,
		       created_at, redacted
		FROM memory_versions
		WHERE store_id = ?
	`
	args := []any{storeID}
	if memoryID != "" {
		query += " AND memory_id = ?"
		args = append(args, memoryID)
	}
	query += " ORDER BY created_at DESC"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []MemoryVersionRow
	for rows.Next() {
		item, err := scanMemoryVersionRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *item)
	}
	return out, rows.Err()
}

// GetVersion returns one version row.
func (r *MemoryStoreRepo) GetVersion(
	ctx context.Context,
	tenantID, storeID, versionID string,
) (*MemoryVersionRow, error) {
	if err := r.requireStore(ctx, tenantID, storeID); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, `
		SELECT id, memory_id, store_id, operation, path, content,
		       content_sha256, size_bytes, actor_type, actor_id,
		       created_at, redacted
		FROM memory_versions
		WHERE store_id = ? AND id = ?
	`, storeID, versionID)
	ver, err := scanMemoryVersion(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return ver, err
}

// RedactVersion clears content on a version row.
func (r *MemoryStoreRepo) RedactVersion(
	ctx context.Context,
	tenantID, storeID, versionID string,
) (*MemoryVersionRow, error) {
	if err := r.requireStore(ctx, tenantID, storeID); err != nil {
		return nil, err
	}
	ver, err := r.GetVersion(ctx, tenantID, storeID, versionID)
	if err != nil {
		return nil, err
	}
	if ver == nil {
		return nil, ErrMemoryVersionNotFound
	}
	_, err = r.db.ExecContext(ctx, `
		UPDATE memory_versions
		SET content = NULL, redacted = 1
		WHERE store_id = ? AND id = ?
	`, storeID, versionID)
	if err != nil {
		return nil, err
	}
	return r.GetVersion(ctx, tenantID, storeID, versionID)
}

var (
	// ErrMemoryStoreNotFound is returned when a store is missing.
	ErrMemoryStoreNotFound = errors.New("memory store not found")
	// ErrMemoryNotFound is returned when a memory is missing.
	ErrMemoryNotFound = errors.New("memory not found")
	// ErrMemoryVersionNotFound is returned when a version is missing.
	ErrMemoryVersionNotFound = errors.New("memory version not found")
	// ErrMemoryPreconditionFailed is returned on CAS conflicts.
	ErrMemoryPreconditionFailed = errors.New("precondition failed")
	// ErrMemoryContentTooLarge is returned when content exceeds the cap.
	ErrMemoryContentTooLarge = errors.New("content exceeds 100KB limit")
)

type memoryVersionInput struct {
	MemoryID      string
	StoreID       string
	Operation     string
	Path          *string
	Content       *string
	ContentSHA256 *string
	SizeBytes     *int64
	ActorType     string
	ActorID       string
}

func (r *MemoryStoreRepo) requireStore(
	ctx context.Context,
	tenantID, storeID string,
) error {
	store, err := r.GetStore(ctx, tenantID, storeID)
	if err != nil {
		return err
	}
	if store == nil {
		return ErrMemoryStoreNotFound
	}
	return nil
}

func (r *MemoryStoreRepo) getMemoryByPath(
	ctx context.Context,
	storeID, path string,
) (*MemoryRow, error) {
	row := r.db.QueryRowContext(ctx, memorySelectSQL+`
		WHERE store_id = ? AND path = ?
	`, storeID, path)
	mem, err := scanMemory(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return mem, err
}

func (r *MemoryStoreRepo) insertMemory(
	ctx context.Context,
	tenantID, storeID, path, content, actorType, actorID, operation string,
) (*MemoryRow, error) {
	id := generateMemoryID()
	inline, blobKey, err := r.persistContent(
		tenantID, storeID, id, content, "",
	)
	if err != nil {
		return nil, err
	}
	now := time.Now().UnixMilli()
	sha := hashContent(content)
	etag := sha
	size := int64(len(content))
	_, err = r.db.ExecContext(ctx, `
		INSERT INTO memories (
			id, store_id, path, content, blob_key, content_sha256, etag,
			size_bytes, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, id, storeID, path, inline, nullEmptyString(blobKey), sha, etag,
		size, now, now)
	if err != nil {
		if blobKey != "" && r.blobs != nil {
			_ = r.blobs.Delete(blobKey)
		}
		return nil, err
	}
	versionContent := versionSnapshotContent(content)
	if err := r.insertVersion(ctx, memoryVersionInput{
		MemoryID:      id,
		StoreID:       storeID,
		Operation:     operation,
		Path:          &path,
		Content:       versionContent,
		ContentSHA256: &sha,
		SizeBytes:     &size,
		ActorType:     actorType,
		ActorID:       actorID,
	}); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, memorySelectSQL+` WHERE id = ?`, id)
	mem, err := scanMemory(row)
	if err != nil {
		return nil, err
	}
	if err := r.hydrateMemory(mem); err != nil {
		return nil, err
	}
	return mem, nil
}

func (r *MemoryStoreRepo) updateMemoryContent(
	ctx context.Context,
	tenantID string,
	existing *MemoryRow,
	content, actorType, actorID, operation string,
) (*MemoryRow, error) {
	inline, blobKey, err := r.persistContent(
		tenantID, existing.StoreID, existing.ID, content, existing.BlobKey,
	)
	if err != nil {
		return nil, err
	}
	sha := hashContent(content)
	etag := sha
	now := time.Now().UnixMilli()
	size := int64(len(content))
	_, err = r.db.ExecContext(ctx, `
		UPDATE memories
		SET content = ?, blob_key = ?, content_sha256 = ?, etag = ?,
		    size_bytes = ?, updated_at = ?
		WHERE id = ?
	`, inline, nullEmptyString(blobKey), sha, etag, size, now, existing.ID)
	if err != nil {
		return nil, err
	}
	path := existing.Path
	versionContent := versionSnapshotContent(content)
	if err := r.insertVersion(ctx, memoryVersionInput{
		MemoryID:      existing.ID,
		StoreID:       existing.StoreID,
		Operation:     operation,
		Path:          &path,
		Content:       versionContent,
		ContentSHA256: &sha,
		SizeBytes:     &size,
		ActorType:     actorType,
		ActorID:       actorID,
	}); err != nil {
		return nil, err
	}
	row := r.db.QueryRowContext(ctx, memorySelectSQL+` WHERE id = ?`, existing.ID)
	mem, err := scanMemory(row)
	if err != nil {
		return nil, err
	}
	if err := r.hydrateMemory(mem); err != nil {
		return nil, err
	}
	return mem, nil
}

func (r *MemoryStoreRepo) insertVersion(
	ctx context.Context,
	in memoryVersionInput,
) error {
	id := generateMemoryVersionID()
	now := time.Now().UnixMilli()
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO memory_versions (
			id, memory_id, store_id, operation, path, content,
			content_sha256, size_bytes, actor_type, actor_id,
			created_at, redacted
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0)
	`, id, in.MemoryID, in.StoreID, in.Operation,
		nullStringPtr(in.Path), nullStringPtr(in.Content),
		nullStringPtr(in.ContentSHA256), nullInt64Ptr(in.SizeBytes),
		in.ActorType, in.ActorID, now)
	return err
}

func hashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

func generateMemoryStoreID() string {
	return "memstore-" + randomString(idLength)
}

func generateMemoryID() string {
	return "mem-" + randomString(idLength)
}

func generateMemoryVersionID() string {
	return "memver-" + randomString(idLength)
}

func nullSQLString(s sql.NullString) any {
	if s.Valid {
		return s.String
	}
	return nil
}

func nullStringPtr(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

func nullInt64Ptr(n *int64) sql.NullInt64 {
	if n == nil {
		return sql.NullInt64{}
	}
	return sql.NullInt64{Int64: *n, Valid: true}
}

func scanMemoryStore(row *sql.Row) (*MemoryStoreRow, error) {
	var s MemoryStoreRow
	err := row.Scan(
		&s.ID, &s.TenantID, &s.Name, &s.Description,
		&s.CreatedAt, &s.UpdatedAt, &s.ArchivedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func scanMemoryStoreRows(rows *sql.Rows) (*MemoryStoreRow, error) {
	var s MemoryStoreRow
	err := rows.Scan(
		&s.ID, &s.TenantID, &s.Name, &s.Description,
		&s.CreatedAt, &s.UpdatedAt, &s.ArchivedAt,
	)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

func scanMemory(row *sql.Row) (*MemoryRow, error) {
	var m MemoryRow
	var blobKey sql.NullString
	err := row.Scan(
		&m.ID, &m.StoreID, &m.Path, &m.Content, &blobKey,
		&m.ContentSHA256, &m.ETag, &m.SizeBytes, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if blobKey.Valid {
		m.BlobKey = blobKey.String
	}
	return &m, nil
}

func scanMemoryRows(rows *sql.Rows) (*MemoryRow, error) {
	var m MemoryRow
	var blobKey sql.NullString
	err := rows.Scan(
		&m.ID, &m.StoreID, &m.Path, &m.Content, &blobKey,
		&m.ContentSHA256, &m.ETag, &m.SizeBytes, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	if blobKey.Valid {
		m.BlobKey = blobKey.String
	}
	return &m, nil
}

func scanMemoryVersion(row *sql.Row) (*MemoryVersionRow, error) {
	var v MemoryVersionRow
	var redacted int
	err := row.Scan(
		&v.ID, &v.MemoryID, &v.StoreID, &v.Operation, &v.Path,
		&v.Content, &v.ContentSHA256, &v.SizeBytes,
		&v.ActorType, &v.ActorID, &v.CreatedAt, &redacted,
	)
	if err != nil {
		return nil, err
	}
	v.Redacted = redacted != 0
	return &v, nil
}

func scanMemoryVersionRows(rows *sql.Rows) (*MemoryVersionRow, error) {
	var v MemoryVersionRow
	var redacted int
	err := rows.Scan(
		&v.ID, &v.MemoryID, &v.StoreID, &v.Operation, &v.Path,
		&v.Content, &v.ContentSHA256, &v.SizeBytes,
		&v.ActorType, &v.ActorID, &v.CreatedAt, &redacted,
	)
	if err != nil {
		return nil, err
	}
	v.Redacted = redacted != 0
	return &v, nil
}
