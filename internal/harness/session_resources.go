package harness

import (
	"context"
	"encoding/json"

	"github.com/open-ma/oma-building/internal/store"
)

// ScopeSessionResources creates session-scoped file copies for file-type
// resources. Missing files are skipped silently per AMA convention.
func (r *ResourceResolver) ScopeSessionResources(
	ctx context.Context,
	tenantID, sessionID string,
	specs []map[string]any,
) []map[string]any {
	if r == nil || len(specs) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(specs))
	for _, spec := range specs {
		resType, _ := spec["type"].(string)
		if resType != "file" {
			out = append(out, spec)
			continue
		}
		scoped, ok := r.scopeOneFile(ctx, tenantID, sessionID, spec)
		if ok {
			out = append(out, scoped)
		}
	}
	return out
}

func (r *ResourceResolver) scopeOneFile(
	ctx context.Context,
	tenantID, sessionID string,
	spec map[string]any,
) (map[string]any, bool) {
	if r == nil || r.Files == nil {
		return nil, false
	}
	fileID, _ := spec["file_id"].(string)
	if fileID == "" {
		return nil, false
	}
	row, err := r.Files.Get(ctx, tenantID, fileID)
	if err != nil || row == nil {
		return nil, false
	}
	scopedID := store.NewFileID()
	sessID := sessionID
	_, err = r.Files.Insert(ctx, store.CreateFileInput{
		ID:           scopedID,
		TenantID:     tenantID,
		SessionID:    &sessID,
		Filename:     row.Filename,
		MediaType:    row.MediaType,
		SizeBytes:    row.SizeBytes,
		Downloadable: row.Downloadable,
		BlobKey:      row.BlobKey,
	})
	if err != nil {
		return nil, false
	}
	scoped := make(map[string]any, len(spec)+1)
	for k, v := range spec {
		scoped[k] = v
	}
	scoped["file_id"] = scopedID
	return scoped, true
}

// ParseResourceArray unmarshals a JSON array of resource specs.
func ParseResourceArray(raw json.RawMessage) []map[string]any {
	if len(raw) == 0 || string(raw) == "null" || string(raw) == "[]" {
		return nil
	}
	var items []any
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil
	}
	return CoerceResourceMaps(items)
}

func mergeResourceSpecs(
	envSnapshot json.RawMessage,
	sessionResources json.RawMessage,
) []map[string]any {
	envSpecs := parseResourceSpecs(envSnapshot)
	sessionSpecs := ParseResourceArray(sessionResources)
	if len(envSpecs) == 0 && len(sessionSpecs) == 0 {
		return nil
	}

	byKey := make(map[string]map[string]any, len(envSpecs)+len(sessionSpecs))
	order := make([]string, 0, len(envSpecs)+len(sessionSpecs))

	add := func(spec map[string]any) {
		key := resourceSpecKey(spec)
		if _, seen := byKey[key]; !seen {
			order = append(order, key)
		}
		byKey[key] = spec
	}

	for _, spec := range envSpecs {
		add(spec)
	}
	for _, spec := range sessionSpecs {
		add(spec)
	}

	out := make([]map[string]any, 0, len(order))
	for _, key := range order {
		out = append(out, byKey[key])
	}
	return out
}

func resourceSpecKey(spec map[string]any) string {
	if mp := strAny(spec["mount_path"]); mp != "" {
		return "path:" + mp
	}
	resType, _ := spec["type"].(string)
	if resType == "env" || resType == "env_secret" {
		if name := strAny(spec["name"]); name != "" {
			return "env:" + name
		}
	}
	if fid := strAny(spec["file_id"]); fid != "" {
		return "file:" + fid
	}
	if sid := strAny(spec["memory_store_id"], spec["id"]); sid != "" {
		return "mem:" + sid
	}
	raw, _ := json.Marshal(spec)
	return "raw:" + string(raw)
}
