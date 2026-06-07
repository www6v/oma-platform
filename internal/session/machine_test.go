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

func TestTurnMarksSessionRunningThenIdle(t *testing.T) {
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
	hub := stream.NewHub()
	workdirs := workdir.NewManager(t.TempDir())
	ctx := context.Background()

	agent, err := agents.Create(ctx, store.CreateAgentInput{
		Name:  "machine-agent",
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
		Hub:       hub,
		Workdirs:  workdirs,
		Harness:   &harness.FakeClient{Text: "hello"},
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
		t.Fatal(err)
	}

	got, err := sessions.Get(ctx, "default", sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.SessionStatusIdle {
		t.Fatalf("status=%s", got.Status)
	}
	if got.TurnID != nil {
		t.Fatalf("turn_id=%v", got.TurnID)
	}

	list, err := events.ListEvents(ctx, sess.ID, 0, 100, true)
	if err != nil {
		t.Fatal(err)
	}
	foundAgent := false
	for _, ev := range list {
		if ev.Type == "agent.message" {
			foundAgent = true
		}
	}
	if !foundAgent {
		t.Fatal("expected agent.message event")
	}
}

func TestRegistryEnqueueRunsAsync(t *testing.T) {
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
	hub := stream.NewHub()
	workdirs := workdir.NewManager(t.TempDir())
	ctx := context.Background()

	agent, err := agents.Create(ctx, store.CreateAgentInput{
		Name:  "reg-agent",
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
		Hub:       hub,
		Workdirs:  workdirs,
		Harness:   &harness.FakeClient{},
	}
	reg := session.NewRegistry()
	reg.Register(sess.ID, machine)

	userEvent, _ := json.Marshal(map[string]any{
		"type": "user.message",
		"content": []map[string]string{
			{"type": "text", "text": "ping"},
		},
	})
	done := make(chan error, 1)
	if err := reg.EnqueueUserMessage(ctx, sess.ID, userEvent, func(err error) {
		done <- err
	}); err != nil {
		t.Fatal(err)
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}
}
