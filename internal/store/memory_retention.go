package store

import (
	"context"
	"database/sql"
)

// PruneVersionsOlderThan deletes version rows older than cutoffMs except the
// most recent version per memory_id.
func (r *MemoryStoreRepo) PruneVersionsOlderThan(
	ctx context.Context,
	cutoffMs int64,
) (int64, error) {
	res, err := r.db.ExecContext(ctx, `
		DELETE FROM memory_versions
		WHERE created_at < ?
		  AND id NOT IN (
		    SELECT v.id FROM memory_versions v
		    INNER JOIN (
		      SELECT memory_id, MAX(created_at) AS max_at
		      FROM memory_versions
		      GROUP BY memory_id
		    ) latest
		      ON v.memory_id = latest.memory_id
		     AND v.created_at = latest.max_at
		  )
	`, cutoffMs)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return -1, nil
	}
	return n, nil
}

// ListMemoryBlobKeys returns blob keys for memories in a store.
func (r *MemoryStoreRepo) ListMemoryBlobKeys(
	ctx context.Context,
	storeID string,
) ([]string, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT blob_key FROM memories
		WHERE store_id = ? AND blob_key IS NOT NULL AND blob_key != ''
	`, storeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key sql.NullString
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		if key.Valid && key.String != "" {
			keys = append(keys, key.String)
		}
	}
	return keys, rows.Err()
}
