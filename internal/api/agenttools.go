package api

import (
	"encoding/json"
	"fmt"
)

// validateAgentTools ensures tools is a JSON array of tool declaration objects.
// Omitted, null, or empty tools are allowed.
func validateAgentTools(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	trimmed := string(raw)
	if trimmed == "null" {
		return nil
	}

	var items []json.RawMessage
	if err := json.Unmarshal(raw, &items); err != nil {
		return fmt.Errorf("tools must be a JSON array of objects")
	}

	for i, item := range items {
		var decl map[string]json.RawMessage
		if err := json.Unmarshal(item, &decl); err != nil {
			return fmt.Errorf("tools[%d] must be an object", i)
		}
		typeRaw, ok := decl["type"]
		if !ok {
			return fmt.Errorf("tools[%d] requires a type field", i)
		}
		var toolType string
		if err := json.Unmarshal(typeRaw, &toolType); err != nil || toolType == "" {
			return fmt.Errorf("tools[%d] requires a non-empty type", i)
		}
	}
	return nil
}
