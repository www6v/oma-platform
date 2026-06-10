package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness"
)

// gapHarness streams events with a gap so SSE can observe partial turn output.
type gapHarness struct {
	gap time.Duration
}

func (g *gapHarness) RunTurn(
	_ context.Context,
	_ harness.TurnRequest,
) (harness.TurnResponse, error) {
	return harness.TurnResponse{}, nil
}

func (g *gapHarness) RunTurnStream(
	ctx context.Context,
	_ harness.TurnRequest,
	onEvent harness.EventHandler,
) error {
	tool, _ := json.Marshal(map[string]any{
		"type": "agent.tool_use",
		"id":   "tool_gap",
		"name": "search",
		"input": map[string]any{
			"q": "test",
		},
	})
	if err := onEvent(tool); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(g.gap):
	}
	msg, _ := json.Marshal(map[string]any{
		"type": "agent.message",
		"content": []map[string]string{
			{"type": "text", "text": "streamed"},
		},
	})
	return onEvent(msg)
}

func TestSessionEventsStreamIncrementalHarness(t *testing.T) {
	handler, _ := testRouterHarness(t, &gapHarness{gap: 400 * time.Millisecond})
	server := httptest.NewServer(handler)
	defer server.Close()

	sid := createAgentSession(t, server.Client(), server.URL)
	streamURL := server.URL + "/v1/sessions/" + sid + "/events/stream"

	partial := make(chan struct{})
	var partialOnce sync.Once
	done := make(chan struct{})
	errCh := make(chan error, 1)
	go func() {
		events, err := readSSEUntil(
			streamURL,
			6*time.Second,
			func(events []sseEvent) bool {
				if hasEventTypes(events, "agent.tool_use") &&
					!lifecyclePhase(events, "turn_end") {
					partialOnce.Do(func() { close(partial) })
				}
				return hasEventTypes(
					events,
					"user.message",
					"agent.tool_use",
					"agent.message",
				) && lifecyclePhase(events, "turn_end")
			},
		)
		if err != nil {
			errCh <- err
			return
		}
		if !hasEventTypes(events, "agent.tool_use", "agent.message") {
			errCh <- context.Canceled
			return
		}
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	postJSON(t, server.Client(), server.URL+"/v1/sessions/"+sid+"/events",
		`{"events":[{"type":"user.message","content":[{"type":"text","text":"hi"}]}]}`,
		http.StatusAccepted)

	select {
	case <-partial:
	case err := <-errCh:
		t.Fatalf("sse before partial: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for incremental tool_use before turn_end")
	}

	select {
	case <-done:
	case err := <-errCh:
		t.Fatalf("sse read: %v", err)
	case <-time.After(6 * time.Second):
		t.Fatal("timeout waiting for full turn_end")
	}
}
