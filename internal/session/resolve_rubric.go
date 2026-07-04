package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/open-ma/oma-building/internal/harness"
)

// ResolveRubric resolves user.define_outcome rubric to plain markdown text.
// Supports legacy bare strings and AMA RubricSpec ({type:text|file}).
func ResolveRubric(
	ctx context.Context,
	tenantID string,
	raw json.RawMessage,
	resources *harness.ResourceResolver,
) (string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return "", fmt.Errorf("no rubric provided")
	}

	var inline string
	if err := json.Unmarshal(raw, &inline); err == nil {
		text := strings.TrimSpace(inline)
		if text == "" {
			return "", fmt.Errorf("rubric is empty")
		}
		return text, nil
	}

	var spec struct {
		Type    string `json:"type"`
		Content string `json:"content"`
		FileID  string `json:"file_id"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		return "", fmt.Errorf("unknown rubric shape")
	}

	switch spec.Type {
	case "text":
		text := strings.TrimSpace(spec.Content)
		if text == "" {
			return "", fmt.Errorf("rubric.content is empty")
		}
		return text, nil
	case "file":
		return resolveRubricFile(ctx, tenantID, spec.FileID, resources)
	default:
		return "", fmt.Errorf("unknown rubric type %q", spec.Type)
	}
}

func resolveRubricFile(
	ctx context.Context,
	tenantID, fileID string,
	resources *harness.ResourceResolver,
) (string, error) {
	if fileID == "" {
		return "", fmt.Errorf("rubric.file_id is empty")
	}
	if resources == nil || resources.Files == nil || resources.FileBlobs == nil {
		return "", fmt.Errorf(
			"rubric file fetch failed: file repos unavailable",
		)
	}
	row, err := resources.Files.Get(ctx, tenantID, fileID)
	if err != nil {
		return "", fmt.Errorf("rubric file fetch failed: %v", err)
	}
	if row == nil {
		return "", fmt.Errorf(
			"rubric file fetch failed: not found (file_id=%s)", fileID,
		)
	}
	data, err := resources.FileBlobs.Read(row.BlobKey)
	if err != nil {
		return "", fmt.Errorf("rubric file fetch failed: %v", err)
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return "", fmt.Errorf(
			"rubric file fetch failed: empty body (file_id=%s)", fileID,
		)
	}
	return text, nil
}

func hasRubricSpec(raw any) bool {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v) != ""
	case map[string]any:
		t, _ := v["type"].(string)
		switch t {
		case "text":
			c, _ := v["content"].(string)
			return strings.TrimSpace(c) != ""
		case "file":
			id, _ := v["file_id"].(string)
			return id != ""
		}
	}
	return false
}

func rubricRawFromAny(raw any) json.RawMessage {
	if raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return nil
		}
		out, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		return out
	default:
		out, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		return out
	}
}
