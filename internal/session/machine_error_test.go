package session_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/session"
	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/stream"
	"github.com/open-ma/oma-building/internal/workdir"
)

type failingHarness struct{}

func (failingHarness) RunTurn(
	_ context.Context,
	_ harness.TurnRequest,
) (harness.TurnResponse, error) {
	return harness.TurnResponse{}, errors.New("harness unavailable")
}

func TestRunTurnHarnessFailureEmitsSessionError(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	ctx := context.Background()
	agents := store.NewAgentRepo(db)
	environments := store.NewEnvironmentRepo(db)
	if err := environments.EnsureDefault(ctx); err != nil {
		t.Fatal(err)
	}
	sessions := store.NewSessionRepo(db, agents, environments)
	events := store.NewEventRepo(db)

	agent, err := agents.Create(ctx, store.CreateAgentInput{
		Name:  "fail-agent",
		Model: "faux/test",
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.Create(ctx, store.CreateSessionInput{AgentID: agent.ID})
	if err != nil {
		t.Fatal(err)
	}

	machine := &session.Machine{
		TenantID:  "default",
		SessionID: sess.ID,
		Sessions:  sessions,
		Events:    events,
		Hub:       stream.NewHub(),
		Workdirs:  workdir.NewManager(t.TempDir(), ""),
		Harness:   failingHarness{},
	}

	userEvent, _ := json.Marshal(map[string]any{
		"type": "user.message",
		"content": []map[string]string{
			{"type": "text", "text": "hi"},
		},
	})
	if _, err := events.AppendEvents(ctx, sess.ID, []json.RawMessage{userEvent}); err != nil {
		t.Fatal(err)
	}
	if err := machine.RunTurn(ctx); err != nil {
		t.Fatalf("RunTurn returned error: %v", err)
	}

	got, err := sessions.Get(ctx, "default", sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.SessionStatusIdle {
		t.Fatalf("status=%s", got.Status)
	}

	list, err := events.ListEvents(ctx, sess.ID, 0, 100, true)
	if err != nil {
		t.Fatal(err)
	}

	var hasStart, hasError, hasEnd bool
	for _, ev := range list {
		switch ev.Type {
		case "session.lifecycle":
			var payload map[string]any
			_ = json.Unmarshal(ev.Payload, &payload)
			switch payload["phase"] {
			case "turn_start":
				hasStart = true
			case "turn_end":
				hasEnd = true
			}
		case "session.error":
			hasError = true
		}
	}
	if !hasStart || !hasError || !hasEnd {
		t.Fatalf(
			"events start=%v error=%v end=%v types=%v",
			hasStart,
			hasError,
			hasEnd,
			eventTypes(list),
		)
	}
}

func eventTypes(list []store.StoredEvent) []string {
	out := make([]string, 0, len(list))
	for _, ev := range list {
		out = append(out, ev.Type)
	}
	return out
}
