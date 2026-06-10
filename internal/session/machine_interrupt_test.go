package session_test

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/session"
	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/stream"
	"github.com/open-ma/oma-building/internal/workdir"
)

type slowHarness struct {
	delay time.Duration

	started atomic.Int32
}

func (s *slowHarness) RunTurn(
	ctx context.Context,
	req harness.TurnRequest,
) (harness.TurnResponse, error) {
	s.started.Add(1)
	select {
	case <-ctx.Done():
		return harness.TurnResponse{}, ctx.Err()
	case <-time.After(s.delay):
	}
	fc := &harness.FakeClient{Text: "done"}
	return fc.RunTurn(ctx, req)
}

func (s *slowHarness) RunTurnStream(
	ctx context.Context,
	req harness.TurnRequest,
	onEvent harness.EventHandler,
) error {
	s.started.Add(1)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(s.delay):
	}
	fc := &harness.FakeClient{Text: "done"}
	return fc.RunTurnStream(ctx, req, onEvent)
}

func TestInterruptCancelsActiveTurn(t *testing.T) {
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
	hub := stream.NewHub()
	workdirs := workdir.NewManager(t.TempDir())

	agent, err := agents.Create(ctx, store.CreateAgentInput{
		Name:  "interrupt-agent",
		Model: "faux/test",
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.Create(ctx, store.CreateSessionInput{AgentID: agent.ID})
	if err != nil {
		t.Fatal(err)
	}

	slow := &slowHarness{delay: 2 * time.Second}
	machine := &session.Machine{
		TenantID:  "default",
		SessionID: sess.ID,
		Sessions:  sessions,
		Events:    events,
		Hub:       hub,
		Workdirs:  workdirs,
		Harness:   slow,
	}
	reg := session.NewRegistry()
	reg.Register(sess.ID, machine)

	userEvent, _ := json.Marshal(map[string]any{
		"type": "user.message",
		"content": []map[string]string{
			{"type": "text", "text": "go"},
		},
	})
	turnStarted := make(chan struct{})
	if err := reg.EnqueueUserMessage(ctx, sess.ID, userEvent, func(error) {
		close(turnStarted)
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for slow.started.Load() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("timeout waiting for harness turn to start")
		}
		time.Sleep(10 * time.Millisecond)
	}

	interruptEvent, _ := json.Marshal(map[string]any{"type": "user.interrupt"})
	if err := reg.EnqueueEvents(
		ctx, sess.ID, []json.RawMessage{interruptEvent}, false, true, nil,
	); err != nil {
		t.Fatal(err)
	}

	select {
	case <-turnStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for cancelled turn to finish")
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

	var hasIdle, hasError, hasAgentMessage bool
	for _, ev := range list {
		switch ev.Type {
		case "session.status_idle":
			hasIdle = true
		case "session.error":
			hasError = true
		case "agent.message":
			hasAgentMessage = true
		}
	}
	if !hasIdle {
		t.Fatal("expected session.status_idle after interrupt")
	}
	if hasError {
		t.Fatal("interrupt must not emit session.error")
	}
	if hasAgentMessage {
		t.Fatal("cancelled turn should not emit agent.message")
	}
}

func TestInterruptDrainsQueuedTurns(t *testing.T) {
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
	hub := stream.NewHub()
	workdirs := workdir.NewManager(t.TempDir())

	agent, err := agents.Create(ctx, store.CreateAgentInput{
		Name:  "queue-agent",
		Model: "faux/test",
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.Create(ctx, store.CreateSessionInput{AgentID: agent.ID})
	if err != nil {
		t.Fatal(err)
	}

	slow := &slowHarness{delay: 200 * time.Millisecond}
	machine := &session.Machine{
		TenantID:  "default",
		SessionID: sess.ID,
		Sessions:  sessions,
		Events:    events,
		Hub:       hub,
		Workdirs:  workdirs,
		Harness:   slow,
	}
	reg := session.NewRegistry()
	reg.Register(sess.ID, machine)

	userEvent, _ := json.Marshal(map[string]any{
		"type": "user.message",
		"content": []map[string]string{
			{"type": "text", "text": "ping"},
		},
	})
	for i := 0; i < 3; i++ {
		if err := reg.EnqueueUserMessage(ctx, sess.ID, userEvent, nil); err != nil {
			t.Fatal(err)
		}
	}

	interruptEvent, _ := json.Marshal(map[string]any{"type": "user.interrupt"})
	if err := reg.EnqueueEvents(
		ctx, sess.ID, []json.RawMessage{interruptEvent}, false, true, nil,
	); err != nil {
		t.Fatal(err)
	}

	time.Sleep(500 * time.Millisecond)
	if slow.started.Load() != 1 {
		t.Fatalf("started turns=%d, want 1", slow.started.Load())
	}
}

func TestNoOpInterruptDoesNotEmitIdle(t *testing.T) {
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
		Name:  "noop-agent",
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
		Workdirs:  workdir.NewManager(t.TempDir()),
		Harness:   &harness.FakeClient{},
	}
	reg := session.NewRegistry()
	reg.Register(sess.ID, machine)

	interruptEvent, _ := json.Marshal(map[string]any{"type": "user.interrupt"})
	if err := reg.EnqueueEvents(
		ctx, sess.ID, []json.RawMessage{interruptEvent}, false, true, nil,
	); err != nil {
		t.Fatal(err)
	}

	list, err := events.ListEvents(ctx, sess.ID, 0, 100, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range list {
		if ev.Type == "session.status_idle" {
			t.Fatal("no-op interrupt should not emit session.status_idle")
		}
	}
}
