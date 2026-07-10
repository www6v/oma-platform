package harness

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-ma/oma-building/internal/store"
)

// MaterializeMemoryStore writes DB memories to memoryRoot/storeID when missing
// on disk. Existing files are left intact so agent edits persist across turns.
func MaterializeMemoryStore(
	ctx context.Context,
	tenantID, storeID, targetDir string,
	repo *store.MemoryStoreRepo,
) error {
	if repo == nil || storeID == "" || targetDir == "" {
		return nil
	}
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("mkdir memory store dir: %w", err)
	}
	rows, err := repo.ListMemories(ctx, tenantID, storeID, "")
	if err != nil {
		return fmt.Errorf("list memories: %w", err)
	}
	for i := range rows {
		row := rows[i]
		rel := strings.TrimPrefix(row.Path, "/")
		if rel == "" {
			continue
		}
		dest := filepath.Join(targetDir, filepath.FromSlash(rel))
		if _, statErr := os.Stat(dest); statErr == nil {
			continue
		}
		content := row.Content
		if content == "" {
			hydrated, getErr := repo.GetMemory(
				ctx, tenantID, storeID, row.ID,
			)
			if getErr == nil && hydrated != nil {
				content = hydrated.Content
			}
		}
		if strings.TrimSpace(content) == "" {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return fmt.Errorf("mkdir memory path %s: %w", dest, err)
		}
		if err := os.WriteFile(dest, []byte(content), 0o644); err != nil {
			return fmt.Errorf("write memory file %s: %w", dest, err)
		}
	}
	return nil
}
