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

func TestEnqueueEventsPromotesWhenIdle(t *testing.T) {
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
		Name:  "wakeup-agent",
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
		Harness:   &harness.FakeClient{Text: "ok"},
	}
	reg := session.NewRegistry()
	reg.Register(sess.ID, machine)

	userEvent, err := json.Marshal(map[string]any{
		"type": "user.message",
		"content": []map[string]string{
			{"type": "text", "text": "schedule wakeup"},
		},
		"metadata": map[string]any{
			"harness": "schedule",
			"kind":    "wakeup",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	done := make(chan error, 1)
	if err := reg.EnqueueEvents(
		ctx, sess.ID, []json.RawMessage{userEvent}, true, false,
		func(err error) { done <- err },
	); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		rows, err := pending.List(ctx, sess.ID, "sthr_primary", false)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	rows, err := pending.List(ctx, sess.ID, "sthr_primary", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected pending drained while idle, got %d", len(rows))
	}

	list, err := events.ListEvents(ctx, sess.ID, 0, 100, true)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, ev := range list {
		if ev.Type != "user.message" {
			continue
		}
		found = true
		break
	}
	if !found {
		t.Fatal("expected promoted user.message in session_events")
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("turn: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for harness turn")
	}
}
