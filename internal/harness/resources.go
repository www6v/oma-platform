package harness

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/open-ma/oma-building/internal/fileblob"
	"github.com/open-ma/oma-building/internal/store"
)

// ResourceResolver enriches environment resource specs for harness mounting.
type ResourceResolver struct {
	Files        *store.FileRepo
	FileBlobs    *fileblob.Store
	MemoryStores *store.MemoryStoreRepo
}

// ResolveForTurn returns harness-ready resource payloads from an environment
// snapshot. Best-effort: individual resource failures are skipped.
func (r *ResourceResolver) ResolveForTurn(
	ctx context.Context,
	tenantID string,
	envSnapshot json.RawMessage,
) ([]json.RawMessage, error) {
	specs := parseResourceSpecs(envSnapshot)
	if len(specs) == 0 || r == nil {
		return nil, nil
	}
	out := make([]json.RawMessage, 0, len(specs))
	for _, spec := range specs {
		mounted, err := r.resolveOne(ctx, tenantID, spec)
		if err != nil || mounted == nil {
			continue
		}
		raw, err := json.Marshal(mounted)
		if err != nil {
			continue
		}
		out = append(out, raw)
	}
	return out, nil
}

func parseResourceSpecs(envSnapshot json.RawMessage) []map[string]any {
	if len(envSnapshot) == 0 {
		return nil
	}
	var root map[string]any
	if err := json.Unmarshal(envSnapshot, &root); err != nil {
		return nil
	}
	if top, ok := root["resources"].([]any); ok {
		return coerceResourceMaps(top)
	}
	cfg, _ := root["config"].(map[string]any)
	if cfg == nil {
		return nil
	}
	raw, ok := cfg["resources"].([]any)
	if !ok {
		return nil
	}
	return coerceResourceMaps(raw)
}

func coerceResourceMaps(items []any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, m)
	}
	return out
}

func (r *ResourceResolver) resolveOne(
	ctx context.Context,
	tenantID string,
	spec map[string]any,
) (map[string]any, error) {
	resType, _ := spec["type"].(string)
	switch resType {
	case "file":
		return r.resolveFile(ctx, tenantID, spec)
	case "memory_store":
		return r.resolveMemoryStore(ctx, tenantID, spec)
	case "env", "env_secret":
		return resolveEnvResource(spec), nil
	case "github_repository", "github_repo":
		return map[string]any{
			"type":        "github_repository",
			"url":         strAny(spec["url"], spec["repo_url"]),
			"mount_path":  strAny(spec["mount_path"], "/workspace"),
			"branch":      strAny(spec["branch"]),
			"commit":      strAny(spec["commit"]),
			"read_only":   spec["access"] == "read_only",
		}, nil
	default:
		return nil, fmt.Errorf("unsupported resource type %q", resType)
	}
}

func (r *ResourceResolver) resolveFile(
	ctx context.Context,
	tenantID string,
	spec map[string]any,
) (map[string]any, error) {
	if r == nil || r.Files == nil || r.FileBlobs == nil {
		return nil, fmt.Errorf("file repos unavailable")
	}
	fileID, _ := spec["file_id"].(string)
	if fileID == "" {
		return nil, fmt.Errorf("file resource missing file_id")
	}
	row, err := r.Files.Get(ctx, tenantID, fileID)
	if err != nil || row == nil {
		return nil, fmt.Errorf("file %s not found", fileID)
	}
	data, err := r.FileBlobs.Read(row.BlobKey)
	if err != nil {
		return nil, err
	}
	mountPath := strAny(spec["mount_path"])
	if mountPath == "" {
		mountPath = fmt.Sprintf("/mnt/session/uploads/%s", fileID)
	}
	return map[string]any{
		"type":           "file",
		"file_id":        fileID,
		"mount_path":     mountPath,
		"content_base64": base64.StdEncoding.EncodeToString(data),
	}, nil
}

func (r *ResourceResolver) resolveMemoryStore(
	ctx context.Context,
	tenantID string,
	spec map[string]any,
) (map[string]any, error) {
	if r == nil || r.MemoryStores == nil {
		return nil, fmt.Errorf("memory store repo unavailable")
	}
	storeID, _ := spec["memory_store_id"].(string)
	if storeID == "" {
		storeID, _ = spec["id"].(string)
	}
	if storeID == "" {
		return nil, fmt.Errorf("memory_store resource missing id")
	}
	meta, err := r.MemoryStores.GetStore(ctx, tenantID, storeID)
	if err != nil || meta == nil {
		return nil, fmt.Errorf("memory store %s not found", storeID)
	}
	rows, err := r.MemoryStores.ListMemories(ctx, tenantID, storeID, "")
	if err != nil {
		return nil, err
	}
	memories := make([]map[string]string, 0, len(rows))
	for i := range rows {
		row := rows[i]
		content := row.Content
		if content == "" {
			hydrated, err := r.MemoryStores.GetMemory(
				ctx, tenantID, storeID, row.ID,
			)
			if err == nil && hydrated != nil {
				content = hydrated.Content
			}
		}
		memories = append(memories, map[string]string{
			"path":    row.Path,
			"content": content,
		})
	}
	access, _ := spec["access"].(string)
	return map[string]any{
		"type":        "memory_store",
		"store_id":    storeID,
		"store_name":  meta.Name,
		"read_only":   access == "read_only",
		"memories":    memories,
	}, nil
}

func resolveEnvResource(spec map[string]any) map[string]any {
	name, _ := spec["name"].(string)
	value, _ := spec["value"].(string)
	return map[string]any{
		"type":  "env",
		"name":  name,
		"value": value,
	}
}

func strAny(values ...any) string {
	for _, v := range values {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return ""
}
