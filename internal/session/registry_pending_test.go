package session_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/session"
	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/stream"
	"github.com/open-ma/oma-building/internal/workdir"
)

type stallClient struct {
	harness.FakeClient
	entered     chan struct{}
	unblock     chan struct{}
	enteredOnce sync.Once
}

func newStallClient() *stallClient {
	return &stallClient{
		entered: make(chan struct{}),
		unblock: make(chan struct{}),
	}
}

func (s *stallClient) RunTurnStream(
	ctx context.Context,
	req harness.TurnRequest,
	onEvent harness.EventHandler,
) error {
	s.enteredOnce.Do(func() { close(s.entered) })
	select {
	case <-s.unblock:
		return s.FakeClient.RunTurnStream(ctx, req, onEvent)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestRegisterPreservesPendingWhileTurnActive(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	agents := store.NewAgentRepo(db)
	environments := store.NewEnvironmentRepo(db)
	if err := environments.EnsureDefault(context.Background()); err != nil {
		t.Fatal(err)
	}
	sessions := store.NewSessionRepo(db, agents, environments)
	events := store.NewEventRepo(db)
	pending := store.NewPendingRepo(db)
	hub := stream.NewHub()
	workdirs := workdir.NewManager(t.TempDir(), "")
	ctx := context.Background()

	agent, err := agents.Create(ctx, store.CreateAgentInput{
		Name:  "pending-agent",
		Model: "faux/test",
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.Create(ctx, store.CreateSessionInput{AgentID: agent.ID})
	if err != nil {
		t.Fatal(err)
	}

	stall := newStallClient()
	machine := &session.Machine{
		TenantID:  "default",
		SessionID: sess.ID,
		Sessions:  sessions,
		Events:    events,
		Pending:   pending,
		Hub:       hub,
		Workdirs:  workdirs,
		Harness:   stall,
	}
	reg := session.NewRegistry()
	reg.Register(sess.ID, machine)

	first, _ := json.Marshal(map[string]any{
		"type": "user.message",
		"content": []map[string]string{
			{"type": "text", "text": "first"},
		},
	})
	if err := reg.EnqueueEvents(
		ctx, sess.ID, []json.RawMessage{first}, true, false, nil,
	); err != nil {
		t.Fatal(err)
	}

	select {
	case <-stall.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for turn to start")
	}

	// Simulate registerMachine on a subsequent HTTP request.
	replacement := &session.Machine{
		TenantID:  "default",
		SessionID: sess.ID,
		Sessions:  sessions,
		Events:    events,
		Pending:   pending,
		Hub:       hub,
		Workdirs:  workdirs,
		Harness:   stall,
	}
	reg.Register(sess.ID, replacement)

	second, _ := json.Marshal(map[string]any{
		"type": "user.message",
		"content": []map[string]string{
			{"type": "text", "text": "second"},
		},
	})
	if err := reg.EnqueueEvents(
		ctx, sess.ID, []json.RawMessage{second}, true, false, nil,
	); err != nil {
		t.Fatal(err)
	}

	rows, err := pending.List(ctx, sess.ID, "sthr_primary", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 pending row while turn active, got %d", len(rows))
	}
	if rows[0].Type != "user.message" {
		t.Fatalf("type=%q", rows[0].Type)
	}

	close(stall.unblock)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		rows, err = pending.List(ctx, sess.ID, "sthr_primary", false)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(rows) != 0 {
		t.Fatalf("expected pending drained after turn, got %d", len(rows))
	}
}
