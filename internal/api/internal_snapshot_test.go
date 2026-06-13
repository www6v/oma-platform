package api

import (
	"encoding/json"
	"testing"

	"github.com/open-ma/oma-building/internal/store"
)

func TestInjectMcpServersIntoSnapshot(t *testing.T) {
	cfg := store.AgentConfig{
		ID:    "agt-test",
		Model: "claude-sonnet-4-20250514",
		Tools: json.RawMessage(`[{"type":"mcp_toolset","mcp_server_name":"existing"}]`),
	}
	out := injectMcpServersIntoSnapshot(cfg, []internalMCPServer{
		{Name: "existing", URL: "https://example.test/a"},
		{Name: "slack", URL: "https://example.test/slack"},
	})
	var servers []map[string]any
	if err := json.Unmarshal(out.MCPServers, &servers); err != nil {
		t.Fatal(err)
	}
	if len(servers) != 2 {
		t.Fatalf("servers=%v", servers)
	}
	var tools []map[string]any
	if err := json.Unmarshal(out.Tools, &tools); err != nil {
		t.Fatal(err)
	}
	if len(tools) != 2 {
		t.Fatalf("tools=%v", tools)
	}
}

func TestAppendToAgentSnapshotSystemPrompt(t *testing.T) {
	cfg := store.AgentConfig{System: "Base prompt"}
	out := appendToAgentSnapshotSystemPrompt(cfg, "Extra protocol")
	if out.System != "Base prompt\n\nExtra protocol" {
		t.Fatalf("system=%q", out.System)
	}
}
