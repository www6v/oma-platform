package demo

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/open-ma/oma-building/internal/harness"
)

const (
	// SkillHarnessMarker is emitted when resolved skills mount + prompt inject.
	SkillHarnessMarker = "skill-harness-injection-ok"
)

// SkillSimulatingClient validates skill payloads on harness turn requests.
type SkillSimulatingClient struct {
	harness.RecordingClient
}

// RunTurn implements harness.Client.
func (c *SkillSimulatingClient) RunTurn(
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
func (c *SkillSimulatingClient) RunTurnStream(
	ctx context.Context,
	req harness.TurnRequest,
	onEvent harness.EventHandler,
) error {
	c.RecordRequest(req)
	if req.Workdir == "" {
		return fmt.Errorf("turn request missing workdir")
	}
	if err := validateSkillTurn(req); err != nil {
		return err
	}
	text := SkillHarnessMarker + ": skills=" + fmt.Sprint(len(req.Skills))
	return emitSkillMessage(onEvent, text)
}

func validateSkillTurn(req harness.TurnRequest) error {
	if len(req.Skills) == 0 {
		return fmt.Errorf("expected resolved skills on turn request")
	}
	for _, raw := range req.Skills {
		var skill map[string]any
		if json.Unmarshal(raw, &skill) != nil {
			continue
		}
		if skill["type"] != "skill" {
			continue
		}
		addition, _ := skill["system_prompt_addition"].(string)
		if addition == "" {
			return fmt.Errorf("skill missing system_prompt_addition")
		}
		if !strings.Contains(addition, "runbook") &&
			!strings.Contains(addition, "Runbook") &&
			!strings.Contains(addition, "PDF") {
			return fmt.Errorf("unexpected skill addition: %q", addition)
		}
		name, _ := skill["name"].(string)
		if name == "" {
			return fmt.Errorf("skill missing name")
		}
		if !skillPayloadHasSkillMD(skill) {
			return fmt.Errorf("skill %q missing SKILL.md bytes", name)
		}
		return nil
	}
	return fmt.Errorf("no skill payload validated")
}

func skillPayloadHasSkillMD(skill map[string]any) bool {
	files, _ := skill["files"].([]any)
	for _, item := range files {
		row, _ := item.(map[string]any)
		filename, _ := row["filename"].(string)
		if filename != "SKILL.md" {
			continue
		}
		b64, _ := row["content_base64"].(string)
		if b64 == "" {
			return false
		}
		data, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return false
		}
		return strings.Contains(strings.ToLower(string(data)), "runbook")
	}
	return false
}

func emitSkillMessage(
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
