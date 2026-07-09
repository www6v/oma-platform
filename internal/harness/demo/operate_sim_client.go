package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/open-ma/oma-building/internal/harness"
)

const (
	// OperateMcpMarker ends the operate cookbook turn after vault MCP validation.
	OperateMcpMarker = "operate-cookbook-vault-mcp-ok"
)

// OperateSimulatingClient validates CMA_operate_in_production: agent declares
// MCP servers without inline tokens and harness receives mcp_proxy_base.
type OperateSimulatingClient struct {
	harness.RecordingClient

	mu    sync.Mutex
	turns int
}

// RunTurn implements harness.Client.
func (c *OperateSimulatingClient) RunTurn(
	ctx context.Context,
	req harness.TurnRequest,
) (harness.TurnResponse, error) {
	var events []json.RawMessage
	err := c.RunTurnStream(ctx, req, func(ev json.RawMessage) error {
		events = append(events, ev)
		return nil
	})
	return harness.TurnResponse{Events: events}, err
}

// RunTurnStream implements harness.StreamingClient.
func (c *OperateSimulatingClient) RunTurnStream(
	ctx context.Context,
	req harness.TurnRequest,
	onEvent harness.EventHandler,
) error {
	c.mu.Lock()
	c.turns++
	turnNum := c.turns
	c.mu.Unlock()

	c.RecordRequest(req)

	if req.Workdir == "" {
		return fmt.Errorf("turn request missing workdir")
	}
	if req.McpProxyBase == "" {
		return fmt.Errorf("expected mcp_proxy_base on harness turn request")
	}
	if err := validateOperateAgentSnapshot(req.Agent); err != nil {
		return err
	}
	if turnNum != 1 {
		return fmt.Errorf("unexpected operate turn %d", turnNum)
	}

	text := OperateMcpMarker + ": vault-backed GitHub MCP turn complete."
	return emitExploreMessage(onEvent, text)
}

func validateOperateAgentSnapshot(agent harness.AgentSnapshot) error {
	raw := agent.MCPServers
	if len(raw) == 0 || string(raw) == "null" {
		return fmt.Errorf("expected mcp_servers on agent snapshot")
	}
	var servers []map[string]any
	if err := json.Unmarshal(raw, &servers); err != nil {
		return fmt.Errorf("parse mcp_servers: %w", err)
	}
	if len(servers) == 0 {
		return fmt.Errorf("expected at least one mcp server")
	}
	for _, srv := range servers {
		if tok, _ := srv["authorization_token"].(string); tok != "" {
			return fmt.Errorf(
				"mcp server must not carry inline authorization_token",
			)
		}
		if url, _ := srv["url"].(string); url == "" {
			return fmt.Errorf("mcp server missing url")
		}
	}
	toolsRaw := agent.Tools
	if len(toolsRaw) == 0 || string(toolsRaw) == "null" {
		return fmt.Errorf("expected tools on agent snapshot")
	}
	if !strings.Contains(string(toolsRaw), "mcp_toolset") {
		return fmt.Errorf("expected mcp_toolset in agent tools")
	}
	return nil
}

// TurnCount returns harness turns executed.
func (c *OperateSimulatingClient) TurnCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.turns
}
