package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

const (
	// GateHitlTurn1Marker is emitted after the first HITL turn (custom tools pending).
	GateHitlTurn1Marker = "gate-cookbook-hitl-turn-1-ok"
	// GateHitlResumeMarker is emitted on resume turns while custom tools remain pending.
	GateHitlResumeMarker = "gate-cookbook-hitl-resume-ok"
	// GateHitlCompleteMarker is emitted after all custom tools are answered.
	GateHitlCompleteMarker = "gate-cookbook-hitl-complete-ok"
	// GateCustomToolDecideID is a fixed custom_tool_use id for integration tests.
	GateCustomToolDecideID = "ctu_decide_r01"
	// GateCustomToolEscalateID is a fixed custom_tool_use id for integration tests.
	GateCustomToolEscalateID = "ctu_escalate_r02"
)

// GateSimulatingClient validates gate HITL flow: turn 1 emits custom tool uses
// without tool_result so the session machine idles with requires_action.
type GateSimulatingClient struct {
	RecordingClient

	mu    sync.Mutex
	turns int
	// Turn1PendingCount overrides turn-1 custom_tool_use count (default 2).
	Turn1PendingCount int
}

// RunTurn implements Client.
func (c *GateSimulatingClient) RunTurn(
	ctx context.Context,
	req TurnRequest,
) (TurnResponse, error) {
	var events []json.RawMessage
	err := c.RunTurnStream(ctx, req, func(ev json.RawMessage) error {
		events = append(events, ev)
		return nil
	})
	pending := PendingCustomToolIDs(events)
	return TurnResponse{
		Events:               events,
		PendingCustomToolIDs: pending,
	}, err
}

// RunTurnStream implements StreamingClient.
func (c *GateSimulatingClient) RunTurnStream(
	ctx context.Context,
	req TurnRequest,
	onEvent EventHandler,
) error {
	c.mu.Lock()
	c.turns++
	turnNum := c.turns
	c.mu.Unlock()

	c.mu.Lock()
	c.requests = append(c.requests, req)
	c.mu.Unlock()

	if req.Workdir == "" {
		return fmt.Errorf("turn request missing workdir")
	}
	if len(req.Resources) == 0 {
		return fmt.Errorf("expected session resources on turn request")
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
func (c *GateSimulatingClient) TurnCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.turns
}

func (c *GateSimulatingClient) streamForTurn(
	turnNum int,
	req TurnRequest,
) ([]map[string]any, error) {
	if turnNum == 1 {
		return c.turn1Stream(), nil
	}
	pending := PendingCustomToolIDs(req.Events)
	if len(pending) > 0 {
		return c.turnResumeStream(len(pending)), nil
	}
	return c.turnCompleteStream(), nil
}

func (c *GateSimulatingClient) turnResumeStream(pendingCount int) []map[string]any {
	return []map[string]any{
		{
			"type": "agent.message",
			"content": []map[string]string{
				{
					"type": "text",
					"text": fmt.Sprintf(
						"%s pending_remaining=%d",
						GateHitlResumeMarker,
						pendingCount,
					),
				},
			},
		},
	}
}

func (c *GateSimulatingClient) turnCompleteStream() []map[string]any {
	return []map[string]any{
		{
			"type": "agent.message",
			"content": []map[string]string{
				{
					"type": "text",
					"text": GateHitlCompleteMarker,
				},
			},
		},
	}
}

func (c *GateSimulatingClient) turn1Stream() []map[string]any {
	count := c.turn1PendingCount()
	stream := make([]map[string]any, 0, count+1)
	for idx := 0; idx < count; idx++ {
		stream = append(stream, map[string]any{
			"type": "agent.custom_tool_use",
			"id":   fmt.Sprintf("ctu_gate_%02d", idx),
			"name": "decide",
			"input": map[string]any{
				"receipt_id": fmt.Sprintf("r%02d", idx+1),
				"action":     "approve",
				"reason":     "test pending window",
			},
		})
	}
	if count == 2 {
		stream[0]["id"] = GateCustomToolDecideID
		stream[0]["name"] = "decide"
		stream[1]["id"] = GateCustomToolEscalateID
		stream[1]["name"] = "escalate"
		stream[1]["input"] = map[string]any{
			"receipt_id": "r02",
			"question":   "category unclear",
		}
	}
	stream = append(stream, map[string]any{
		"type": "agent.message",
		"content": []map[string]string{
			{
				"type": "text",
				"text": fmt.Sprintf(
					"%s pending_custom_tools=%d",
					GateHitlTurn1Marker,
					count,
				),
			},
		},
	})
	return stream
}

func (c *GateSimulatingClient) turn1PendingCount() int {
	if c.Turn1PendingCount > 0 {
		return c.Turn1PendingCount
	}
	return 2
}
