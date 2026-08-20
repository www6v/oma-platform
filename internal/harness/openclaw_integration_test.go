//go:build integration

package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/open-ma/oma-building/internal/store"
)

// Integration tests that hit the real OpenClaw Gateway. Skipped unless
// OMA_OPENCLAW_GATEWAY_URL is set.
//
// Run with:
//
//	OMA_OPENCLAW_GATEWAY_URL=http://124.221.28.203:17772 \
//	OMA_OPENCLAW_TOKEN=bfc0... \
//	go test -tags=integration ./internal/harness/ -run Integration -v

func integrationConfig(t *testing.T) OpenClawConfig {
	t.Helper()
	url := os.Getenv("OMA_OPENCLAW_GATEWAY_URL")
	if url == "" {
		t.Skip("OMA_OPENCLAW_GATEWAY_URL not set — skipping integration test")
	}
	return OpenClawConfig{
		GatewayURL: url,
		Token:      os.Getenv("OMA_OPENCLAW_TOKEN"),
	}
}

func TestIntegration_OpenClawClient_RunTurn(t *testing.T) {
	cfg := integrationConfig(t)
	client := &OpenClawClient{
		GatewayURL: cfg.GatewayURL,
		Token:      cfg.Token,
		Agent:      "openclaw/default",
	}

	resp, err := client.RunTurn(context.Background(), TurnRequest{
		SessionID: "go-integration-1",
		Agent:     AgentSnapshot{SystemPrompt: "Be very brief."},
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"What is 2+2? Answer in one word."}]}`),
		},
	})
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if len(resp.Events) == 0 {
		t.Fatal("no events returned")
	}

	var ev map[string]any
	if err := json.Unmarshal(resp.Events[0], &ev); err != nil {
		t.Fatal(err)
	}
	if ev["type"] != "agent.message" {
		t.Errorf("event type=%v want agent.message", ev["type"])
	}
	t.Logf("agent.message content: %v", ev["content"])
}

func TestIntegration_OpenClawClient_RunTurnStream(t *testing.T) {
	cfg := integrationConfig(t)
	client := &OpenClawClient{
		GatewayURL: cfg.GatewayURL,
		Token:      cfg.Token,
		Agent:      "openclaw/default",
	}

	var events []string
	err := client.RunTurnStream(context.Background(), TurnRequest{
		SessionID: "go-integration-2",
		Agent:     AgentSnapshot{SystemPrompt: "Be very brief."},
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"Count from 1 to 3."}]}`),
		},
	}, func(event json.RawMessage) error {
		var ev map[string]any
		_ = json.Unmarshal(event, &ev)
		content, _ := ev["content"].([]any)
		if len(content) > 0 {
			block, _ := content[0].(map[string]any)
			text, _ := block["text"].(string)
			events = append(events, text)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RunTurnStream: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("no streaming events received")
	}
	t.Logf("stream events: %d, final: %q", len(events), events[len(events)-1])
}

func TestIntegration_OpenClawViaRegistry(t *testing.T) {
	cfg := integrationConfig(t)
	r := NewRegistry(RegistryConfig{
		OpenClaw: &OpenClawClient{
			GatewayURL: cfg.GatewayURL,
			Token:      cfg.Token,
			Agent:      OpenclawModel("openclaw"),
		},
	})

	c, err := r.ClientFor(store.AgentConfig{Harness: "openclaw"})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	oc, ok := c.(*OpenClawClient)
	if !ok {
		t.Fatalf("expected *OpenClawClient, got %T", c)
	}
	if oc.Agent != "openclaw/default" {
		t.Errorf("agent=%q want openclaw/default", oc.Agent)
	}

	resp, err := c.RunTurn(context.Background(), TurnRequest{
		SessionID: "go-integration-3",
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"Say OK"}]}`),
		},
	})
	if err != nil {
		t.Fatalf("RunTurn via factory: %v", err)
	}
	if len(resp.Events) == 0 {
		t.Fatal("no events returned via factory-built client")
	}
	fmt.Printf("factory test ok: %d events\n", len(resp.Events))
}
