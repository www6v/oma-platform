package session_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/session"
	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/stream"
	"github.com/open-ma/oma-building/internal/workdir"
)

type gateHitlHarness struct{}

func (g *gateHitlHarness) RunTurn(
	ctx context.Context,
	req harness.TurnRequest,
) (harness.TurnResponse, error) {
	var events []json.RawMessage
	err := g.RunTurnStream(ctx, req, func(ev json.RawMessage) error {
		events = append(events, ev)
		return nil
	})
	return harness.TurnResponse{Events: events}, err
}

func (g *gateHitlHarness) RunTurnStream(
	_ context.Context,
	_ harness.TurnRequest,
	onEvent harness.EventHandler,
) error {
	stream := []map[string]any{
		{
			"type": "agent.custom_tool_use",
			"id":   harness.GateCustomToolDecideID,
			"name": "decide",
			"input": map[string]any{
				"receipt_id": "r01",
				"action":     "approve",
			},
		},
		{
			"type": "agent.custom_tool_use",
			"id":   harness.GateCustomToolEscalateID,
			"name": "escalate",
			"input": map[string]any{
				"receipt_id": "r02",
				"question":   "review",
			},
		},
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

func TestRunTurnIdleRequiresActionWhenCustomToolsPending(t *testing.T) {
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
	pending := store.NewPendingRepo(db)
	hub := stream.NewHub()
	workdirs := workdir.NewManager(t.TempDir(), "")

	agent, err := agents.Create(ctx, store.CreateAgentInput{
		Name:  "gate-agent",
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
		Pending:   pending,
		Hub:       hub,
		Workdirs:  workdirs,
		Harness:   &gateHitlHarness{},
	}
	reg := session.NewRegistry()
	reg.Register(sess.ID, machine)

	userEvent, _ := json.Marshal(map[string]any{
		"type": "user.message",
		"content": []map[string]string{
			{"type": "text", "text": "process receipts"},
		},
	})
	done := make(chan struct{})
	if err := reg.EnqueueUserMessage(ctx, sess.ID, userEvent, func(error) {
		close(done)
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for turn")
	}

	list, err := events.ListEvents(ctx, sess.ID, 0, 100, true)
	if err != nil {
		t.Fatal(err)
	}

	var idlePayload map[string]any
	for _, ev := range list {
		if ev.Type != "session.status_idle" {
			continue
		}
		if err := json.Unmarshal(ev.Payload, &idlePayload); err != nil {
			t.Fatal(err)
		}
	}
	if idlePayload == nil {
		t.Fatal("missing session.status_idle")
	}

	stopReason, ok := idlePayload["stop_reason"].(map[string]any)
	if !ok {
		t.Fatalf("stop_reason=%v", idlePayload["stop_reason"])
	}
	if stopReason["type"] != "requires_action" {
		t.Fatalf("stop_reason.type=%v want requires_action", stopReason["type"])
	}
	if stopReason["action_type"] != "custom_tool_result" {
		t.Fatalf(
			"stop_reason.action_type=%v want custom_tool_result",
			stopReason["action_type"],
		)
	}
	rawIDs, ok := stopReason["event_ids"].([]any)
	if !ok || len(rawIDs) != 2 {
		t.Fatalf("event_ids=%v want 2 ids", stopReason["event_ids"])
	}
}

func TestRunTurnIdleEndTurnWhenNoPendingCustomTools(t *testing.T) {
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
	pending := store.NewPendingRepo(db)
	hub := stream.NewHub()
	workdirs := workdir.NewManager(t.TempDir(), "")

	agent, err := agents.Create(ctx, store.CreateAgentInput{
		Name:  "plain-agent",
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
		Pending:   pending,
		Hub:       hub,
		Workdirs:  workdirs,
		Harness:   &harness.FakeClient{Text: "done"},
	}
	reg := session.NewRegistry()
	reg.Register(sess.ID, machine)

	userEvent, _ := json.Marshal(map[string]any{
		"type": "user.message",
		"content": []map[string]string{
			{"type": "text", "text": "hello"},
		},
	})
	done := make(chan struct{})
	if err := reg.EnqueueUserMessage(ctx, sess.ID, userEvent, func(error) {
		close(done)
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for turn")
	}

	list, err := events.ListEvents(ctx, sess.ID, 0, 100, true)
	if err != nil {
		t.Fatal(err)
	}

	var idlePayload map[string]any
	for _, ev := range list {
		if ev.Type != "session.status_idle" {
			continue
		}
		if err := json.Unmarshal(ev.Payload, &idlePayload); err != nil {
			t.Fatal(err)
		}
	}
	stopReason, ok := idlePayload["stop_reason"].(map[string]any)
	if !ok {
		t.Fatalf("stop_reason=%v", idlePayload["stop_reason"])
	}
	if stopReason["type"] != "end_turn" {
		t.Fatalf("stop_reason.type=%v want end_turn", stopReason["type"])
	}
}
