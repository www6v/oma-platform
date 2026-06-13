package api

import (
	"encoding/json"

	"github.com/open-ma/oma-building/internal/store"
)

type internalMCPServer struct {
	Name string `json:"name"`
	URL  string `json:"url"`
	Type string `json:"type"`
}

func appendToAgentSnapshotSystemPrompt(
	cfg store.AgentConfig,
	additional string,
) store.AgentConfig {
	trimmed := trimSpace(additional)
	if trimmed == "" {
		return cfg
	}
	existing := cfg.System
	if existing == "" {
		existing = cfg.SystemPrompt
	}
	sep := ""
	if existing != "" && existing[len(existing)-1] != '\n' {
		sep = "\n\n"
	}
	merged := existing + sep + trimmed
	cfg.System = merged
	cfg.SystemPrompt = merged
	return cfg
}

func injectMcpServersIntoSnapshot(
	cfg store.AgentConfig,
	servers []internalMCPServer,
) store.AgentConfig {
	if len(servers) == 0 {
		return cfg
	}
	existingServers := parseMCPServerList(cfg.MCPServers)
	existingTools := parseToolList(cfg.Tools)
	declared := toolsetServerNames(existingTools)

	for _, srv := range servers {
		srvType := srv.Type
		if srvType == "" {
			srvType = "url"
		}
		existingServers = append(existingServers, map[string]any{
			"name": srv.Name,
			"type": srvType,
			"url":  srv.URL,
		})
		if declared[srv.Name] {
			continue
		}
		existingTools = append(existingTools, map[string]any{
			"type":            "mcp_toolset",
			"mcp_server_name": srv.Name,
			"default_config": map[string]any{
				"permission_policy": map[string]any{
					"type": "always_allow",
				},
			},
		})
		declared[srv.Name] = true
	}

	cfg.MCPServers = mustMarshalJSON(existingServers)
	cfg.Tools = mustMarshalJSON(existingTools)
	return cfg
}

func augmentAgentSnapshot(
	snapshot json.RawMessage,
	servers []internalMCPServer,
	additionalSystemPrompt string,
) (json.RawMessage, error) {
	var cfg store.AgentConfig
	if len(snapshot) > 0 && string(snapshot) != "null" {
		if err := json.Unmarshal(snapshot, &cfg); err != nil {
			return nil, err
		}
	}
	cfg = appendToAgentSnapshotSystemPrompt(cfg, additionalSystemPrompt)
	cfg = injectMcpServersIntoSnapshot(cfg, servers)
	return json.Marshal(cfg)
}

func parseMCPServerList(raw json.RawMessage) []map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func parseToolList(raw json.RawMessage) []map[string]any {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var out []map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func toolsetServerNames(tools []map[string]any) map[string]bool {
	out := make(map[string]bool)
	for _, tool := range tools {
		if tool["type"] != "mcp_toolset" {
			continue
		}
		name, _ := tool["mcp_server_name"].(string)
		if name != "" {
			out[name] = true
		}
	}
	return out
}

func mustMarshalJSON(v any) json.RawMessage {
	out, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage("[]")
	}
	return out
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) {
		c := s[start]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		start++
	}
	end := len(s)
	for end > start {
		c := s[end-1]
		if c != ' ' && c != '\t' && c != '\n' && c != '\r' {
			break
		}
		end--
	}
	return s[start:end]
}
