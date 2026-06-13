package store

import (
	"database/sql"
	"errors"
)

func maxAllowedMemoryBytes(blobEnabled bool) int64 {
	if blobEnabled {
		return maxMemoryBlobBytes
	}
	return maxMemoryInlineOnlyBytes
}

func (r *MemoryStoreRepo) persistContent(
	tenantID, storeID, memoryID, content, previousBlobKey string,
) (inline string, blobKey string, err error) {
	size := int64(len(content))
	if size > maxAllowedMemoryBytes(r.blobs != nil) {
		return "", "", ErrMemoryContentTooLarge
	}
	if r.blobs == nil || size <= maxMemoryInlineBytes {
		if previousBlobKey != "" && r.blobs != nil {
			_ = r.blobs.Delete(previousBlobKey)
		}
		return content, "", nil
	}
	key, err := r.blobs.Write(tenantID, storeID, memoryID, []byte(content))
	if err != nil {
		return "", "", err
	}
	if previousBlobKey != "" && previousBlobKey != key {
		_ = r.blobs.Delete(previousBlobKey)
	}
	return "", key, nil
}

func (r *MemoryStoreRepo) hydrateMemory(mem *MemoryRow) error {
	if mem == nil || mem.BlobKey == "" {
		return nil
	}
	if r.blobs == nil {
		return errors.New("memory blob store is not configured")
	}
	data, err := r.blobs.Read(mem.BlobKey)
	if err != nil {
		return err
	}
	mem.Content = string(data)
	return nil
}

func versionSnapshotContent(content string) *string {
	if int64(len(content)) > maxMemoryVersionInlineBytes {
		return nil
	}
	return &content
}

func nullEmptyString(value string) sql.NullString {
	if value == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: value, Valid: true}
}

const memorySelectSQL = `
	SELECT id, store_id, path, content, blob_key, content_sha256, etag,
	       size_bytes, created_at, updated_at
	FROM memories
`
