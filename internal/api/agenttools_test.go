package api

import (
	"encoding/json"
	"testing"
)

func TestValidateAgentToolsAcceptsOmitted(t *testing.T) {
	if err := validateAgentTools(nil); err != nil {
		t.Fatal(err)
	}
	if err := validateAgentTools(json.RawMessage("null")); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAgentToolsAcceptsToolsetArray(t *testing.T) {
	raw := json.RawMessage(`[{"type":"agent_toolset_20260401"}]`)
	if err := validateAgentTools(raw); err != nil {
		t.Fatal(err)
	}
}

func TestValidateAgentToolsRejectsString(t *testing.T) {
	raw := json.RawMessage(`"agent_toolset_20260401"`)
	if err := validateAgentTools(raw); err == nil {
		t.Fatal("expected error for string tools")
	}
}

func TestValidateAgentToolsRejectsObjectWithoutType(t *testing.T) {
	raw := json.RawMessage(`[{"name":"bash"}]`)
	if err := validateAgentTools(raw); err == nil {
		t.Fatal("expected error for missing type")
	}
}
