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
	// SreInvestigateMarker is emitted on turn 1 after skill + resource validation.
	SreInvestigateMarker = "sre-cookbook-investigate-ok"
	// SrePROpenMarker is emitted on turn 2 after open_pull_request is answered.
	SrePROpenMarker = "sre-cookbook-pr-open-ok"
	// SreCompleteMarker is emitted after approval when the incident is resolved.
	SreCompleteMarker = "sre-cookbook-complete-ok"
	// SreCustomToolOpenPRID is a fixed custom_tool_use id for integration tests.
	SreCustomToolOpenPRID = "sre_open_pr_01"
	// SreCustomToolApprovalID is a fixed custom_tool_use id for HITL approval.
	SreCustomToolApprovalID = "sre_approval_01"

	sreLogMount      = "logs/checkout-svc.log"
	sreManifestMount = "infra/k8s/checkout-deploy.yaml"
	sreRunbookMount  = "runbooks/oom.md"
)

// SreSimulatingClient validates SRE incident responder flow: skills + three file
// resources on turn 1, sequential custom tools (open_pr → approval → complete).
type SreSimulatingClient struct {
	harness.RecordingClient

	mu    sync.Mutex
	turns int
}

// RunTurn implements harness.Client.
func (c *SreSimulatingClient) RunTurn(
	ctx context.Context,
	req harness.TurnRequest,
) (harness.TurnResponse, error) {
	var events []json.RawMessage
	err := c.RunTurnStream(ctx, req, func(ev json.RawMessage) error {
		events = append(events, ev)
		return nil
	})
	pending := harness.PendingCustomToolIDs(events)
	return harness.TurnResponse{
		Events:               events,
		PendingCustomToolIDs: pending,
	}, err
}

// RunTurnStream implements harness.StreamingClient.
func (c *SreSimulatingClient) RunTurnStream(
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

	stream, err := c.streamForTurn(turnNum, req)
	if err != nil {
		return err
	}

	for _, item := range stream {
		raw, err := json.Marshal(item)
		if err != nil {
			return err
		}
		if err := onEvent(raw); err != nil {
			return err
		}
	}
	return nil
}

// TurnCount returns how many harness turns ran.
func (c *SreSimulatingClient) TurnCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.turns
}

func (c *SreSimulatingClient) streamForTurn(
	turnNum int,
	req harness.TurnRequest,
) ([]map[string]any, error) {
	switch turnNum {
	case 1:
		if err := validateSreTurn1(req); err != nil {
			return nil, err
		}
		return c.turn1Stream(), nil
	case 2:
		return c.turn2Stream(), nil
	default:
		return c.turnCompleteStream(), nil
	}
}

func validateSreTurn1(req harness.TurnRequest) error {
	if len(req.Skills) == 0 {
		return fmt.Errorf("expected resolved skills on turn request")
	}
	if len(req.Resources) < 3 {
		return fmt.Errorf(
			"expected >=3 session resources, got %d",
			len(req.Resources),
		)
	}
	paths := make([]string, 0, len(req.Resources))
	for _, raw := range req.Resources {
		var res map[string]any
		if json.Unmarshal(raw, &res) != nil {
			continue
		}
		path, _ := res["mount_path"].(string)
		if path != "" {
			paths = append(paths, path)
		}
	}
	for _, want := range []string{
		sreLogMount,
		sreManifestMount,
		sreRunbookMount,
	} {
		if !containsMountPath(paths, want) {
			return fmt.Errorf("missing resource mount %q in %v", want, paths)
		}
	}
	return nil
}

func containsMountPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want || strings.HasSuffix(path, want) {
			return true
		}
	}
	return false
}

func (c *SreSimulatingClient) turn1Stream() []map[string]any {
	return []map[string]any{
		{
			"type": "agent.message",
			"content": []map[string]string{
				{
					"type": "text",
					"text": SreInvestigateMarker +
						": OOMKilled checkout-svc; 128Mi limit in manifest",
				},
			},
		},
		{
			"type": "agent.custom_tool_use",
			"id":   SreCustomToolOpenPRID,
			"name": "open_pull_request",
			"input": map[string]any{
				"title": "Fix checkout-svc OOMKilled crash-loop",
				"body":  "Raise memory limit from 128Mi to 512Mi per runbook",
				"diff":  "--- a/k8s/checkout-deploy.yaml\n+++ b/k8s/checkout-deploy.yaml",
			},
		},
	}
}

func (c *SreSimulatingClient) turn2Stream() []map[string]any {
	return []map[string]any{
		{
			"type": "agent.message",
			"content": []map[string]string{
				{
					"type": "text",
					"text": SrePROpenMarker + ": pr_number=1",
				},
			},
		},
		{
			"type": "agent.custom_tool_use",
			"id":   SreCustomToolApprovalID,
			"name": "request_approval",
			"input": map[string]any{
				"summary": "Raise checkout-svc memory limit 128Mi → 512Mi",
			},
		},
	}
}

func (c *SreSimulatingClient) turnCompleteStream() []map[string]any {
	return []map[string]any{
		{
			"type": "agent.message",
			"content": []map[string]string{
				{
					"type": "text",
					"text": SreCompleteMarker +
						": merge_pull_request pr_number=1 approved",
				},
			},
		},
	}
}
