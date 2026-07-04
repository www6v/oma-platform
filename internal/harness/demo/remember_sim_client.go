package demo

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-ma/oma-building/internal/harness"
)

const (
	// RememberSaveMarker ends session 1 after persisting a preference.
	RememberSaveMarker = "remember-cookbook-save-ok"
	// RememberRecallMarker ends session 2 after recalling the preference.
	RememberRecallMarker = "remember-cookbook-recall-ok"
	// PreferenceMemoryPath is the cookbook preference file path.
	PreferenceMemoryPath = "/preferences/formatting.md"
	// PreferenceMemoryContent is written in session 1 and recalled in session 2.
	PreferenceMemoryContent = "User prefers bullet points and concise replies."
)

// RememberSimulatingClient validates memory_store resources and simulates
// cross-session preference save + recall.
type RememberSimulatingClient struct {
	harness.RecordingClient
}

// RunTurn implements harness.Client.
func (c *RememberSimulatingClient) RunTurn(
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
func (c *RememberSimulatingClient) RunTurnStream(
	ctx context.Context,
	req harness.TurnRequest,
	onEvent harness.EventHandler,
) error {
	if err := validateRememberTurnRequest(req); err != nil {
		return err
	}
	c.RecordRequest(req)

	content, recalled := harness.MemoryContentAtPath(
		req.Resources, PreferenceMemoryPath,
	)
	if recalled && strings.Contains(content, "bullet points") {
		return emitRememberMessage(onEvent, RememberRecallMarker+": "+content)
	}

	storeName := memoryStoreName(req.Resources)
	if storeName == "" {
		return fmt.Errorf("memory_store resource missing store_name")
	}
	if err := writePreferenceFile(req.Workdir, storeName); err != nil {
		return err
	}
	return emitRememberMessage(onEvent, RememberSaveMarker)
}

func validateRememberTurnRequest(req harness.TurnRequest) error {
	if req.Workdir == "" {
		return fmt.Errorf("turn request missing workdir")
	}
	if len(req.Resources) == 0 {
		return fmt.Errorf("expected memory_store resource on turn request")
	}
	hasMemory := false
	for _, raw := range req.Resources {
		var res map[string]any
		if json.Unmarshal(raw, &res) != nil {
			continue
		}
		if res["type"] == "memory_store" {
			hasMemory = true
		}
	}
	if !hasMemory {
		return fmt.Errorf("expected memory_store in resources")
	}
	return nil
}

func memoryStoreName(resources []json.RawMessage) string {
	for _, raw := range resources {
		var res map[string]any
		if json.Unmarshal(raw, &res) != nil {
			continue
		}
		if res["type"] != "memory_store" {
			continue
		}
		name, _ := res["store_name"].(string)
		if name != "" {
			return name
		}
		id, _ := res["store_id"].(string)
		return id
	}
	return ""
}

func writePreferenceFile(workdir, storeName string) error {
	rel := filepath.Join(
		"mnt", "memory", storeName,
		strings.TrimPrefix(PreferenceMemoryPath, "/"),
	)
	target := filepath.Join(workdir, rel)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	return os.WriteFile(
		target,
		[]byte(PreferenceMemoryContent),
		0o644,
	)
}

func emitRememberMessage(
	onEvent harness.EventHandler,
	text string,
) error {
	raw, err := json.Marshal(map[string]any{
		"type": "agent.message",
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	})
	if err != nil {
		return err
	}
	return onEvent(raw)
}
