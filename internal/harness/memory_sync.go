package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-ma/oma-building/internal/store"
)

// MemoryStoreBinding identifies a read-write memory mount to sync after a turn.
type MemoryStoreBinding struct {
	StoreID   string
	StoreName string
	ReadOnly  bool
}

// MemoryStoreBindings extracts memory_store entries from harness resources.
func MemoryStoreBindings(resources []json.RawMessage) []MemoryStoreBinding {
	if len(resources) == 0 {
		return nil
	}
	out := make([]MemoryStoreBinding, 0, len(resources))
	for _, raw := range resources {
		var res map[string]any
		if json.Unmarshal(raw, &res) != nil {
			continue
		}
		if res["type"] != "memory_store" {
			continue
		}
		storeID, _ := res["store_id"].(string)
		if storeID == "" {
			// Unresolved session resources use Anthropic wire name.
			storeID, _ = res["memory_store_id"].(string)
		}
		if storeID == "" {
			continue
		}
		storeName, _ := res["store_name"].(string)
		if storeName == "" {
			storeName, _ = res["name"].(string)
		}
		if storeName == "" {
			storeName = storeID
		}
		readOnly, _ := res["read_only"].(bool)
		if !readOnly {
			if access, _ := res["access"].(string); access == "read_only" {
				readOnly = true
			}
		}
		out = append(out, MemoryStoreBinding{
			StoreID:   storeID,
			StoreName: storeName,
			ReadOnly:  readOnly,
		})
	}
	return out
}

// SyncMemoryStoresFromWorkdir persists agent file writes under
// mnt/memory/<store_name>/ into the memory store API (AMA FUSE parity).
func SyncMemoryStoresFromWorkdir(
	ctx context.Context,
	workdirPath, tenantID, sessionID string,
	bindings []MemoryStoreBinding,
	repo *store.MemoryStoreRepo,
) error {
	if repo == nil || workdirPath == "" || len(bindings) == 0 {
		return nil
	}
	for _, binding := range bindings {
		if binding.ReadOnly || binding.StoreID == "" {
			continue
		}
		root := filepath.Join(
			workdirPath, ".mnt", "memory", binding.StoreName,
		)
		if err := syncMemoryTree(
			ctx, root, tenantID, sessionID, binding.StoreID, repo,
		); err != nil {
			return err
		}
	}
	return nil
}

func syncMemoryTree(
	ctx context.Context,
	root, tenantID, sessionID, storeID string,
	repo *store.MemoryStoreRepo,
) error {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat memory mount %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil
	}
	// workdir .mnt/memory/<store_name> is a symlink to data/memory/<store_id>.
	// filepath.WalkDir uses Lstat and will not descend into symlink roots, so
	// resolve before walking (production mount layout).
	walkRoot := root
	if resolved, evalErr := filepath.EvalSymlinks(root); evalErr == nil {
		walkRoot = resolved
	} else if !os.IsNotExist(evalErr) {
		return fmt.Errorf("eval memory mount %s: %w", root, evalErr)
	}
	return filepath.WalkDir(walkRoot, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(walkRoot, path)
		if err != nil {
			return err
		}
		memPath := "/" + filepath.ToSlash(rel)
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read memory file %s: %w", path, err)
		}
		content := string(data)
		if strings.TrimSpace(content) == "" {
			return nil
		}
		_, err = repo.WriteMemory(
			ctx, tenantID, storeID, memPath, content,
			"agent_session", sessionID, nil,
		)
		if err != nil {
			return fmt.Errorf("write memory %s: %w", memPath, err)
		}
		return nil
	})
}

// MemoryContentAtPath returns memory text from a resolved memory_store resource.
func MemoryContentAtPath(
	resources []json.RawMessage,
	memPath string,
) (string, bool) {
	for _, raw := range resources {
		var res map[string]any
		if json.Unmarshal(raw, &res) != nil {
			continue
		}
		if res["type"] != "memory_store" {
			continue
		}
		memories, ok := res["memories"].([]any)
		if !ok {
			continue
		}
		for _, item := range memories {
			row, ok := item.(map[string]any)
			if !ok {
				continue
			}
			path, _ := row["path"].(string)
			if path != memPath {
				continue
			}
			content, _ := row["content"].(string)
			if content != "" {
				return content, true
			}
		}
	}
	return "", false
}
