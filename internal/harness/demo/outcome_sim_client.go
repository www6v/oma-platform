package demo

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/open-ma/oma-building/internal/harness"
)

const (
	// OutcomeTurn1Marker is emitted on the first harness turn (fails grader).
	OutcomeTurn1Marker = "outcome-cookbook-turn-1"
	// OutcomePassMarker is emitted after outcome_feedback revision turn.
	OutcomePassMarker = "outcome-cookbook-pass-ok"
)

// OutcomeSimulatingClient drives the outcome grader cookbook flow:
// turn 1 output fails FakeClient evaluation; revision turn passes.
type OutcomeSimulatingClient struct {
	harness.RecordingClient

	mu    sync.Mutex
	turns int
}

// RunTurn implements harness.Client.
func (c *OutcomeSimulatingClient) RunTurn(
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
func (c *OutcomeSimulatingClient) RunTurnStream(
	ctx context.Context,
	req harness.TurnRequest,
	onEvent harness.EventHandler,
) error {
	c.mu.Lock()
	c.turns++
	turn := c.turns
	c.mu.Unlock()

	text := OutcomeTurn1Marker + " fail"
	if turn > 1 {
		text = OutcomePassMarker
	}
	c.FakeClient.Text = text
	c.RecordRequest(req)
	return c.FakeClient.RunTurnStream(ctx, req, onEvent)
}

// TurnCount returns how many harness turns ran.
func (c *OutcomeSimulatingClient) TurnCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.turns
}

// EvaluateOutcome delegates to FakeClient (fail substring → needs_revision).
func (c *OutcomeSimulatingClient) EvaluateOutcome(
	ctx context.Context,
	req harness.OutcomeEvaluateRequest,
) (harness.OutcomeEvaluateResponse, error) {
	return c.FakeClient.EvaluateOutcome(ctx, req)
}
