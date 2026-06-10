package session_test

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/session"
	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/stream"
	"github.com/open-ma/oma-building/internal/workdir"
)

type gateHarness struct {
	harness.FakeClient
	delay  time.Duration
	active atomic.Int32
	peak   atomic.Int32
}

func (g *gateHarness) RunTurn(
	ctx context.Context,
	req harness.TurnRequest,
) (harness.TurnResponse, error) {
	cur := g.active.Add(1)
	for {
		peak := g.peak.Load()
		if cur <= peak {
			break
		}
		if g.peak.CompareAndSwap(peak, cur) {
			break
		}
	}
	time.Sleep(g.delay)
	g.active.Add(-1)
	return g.FakeClient.RunTurn(ctx, req)
}

func (g *gateHarness) RunTurnStream(
	ctx context.Context,
	req harness.TurnRequest,
	onEvent harness.EventHandler,
) error {
	resp, err := g.RunTurn(ctx, req)
	if err != nil {
		return err
	}
	for _, ev := range resp.Events {
		if err := onEvent(ev); err != nil {
			return err
		}
	}
	return nil
}

func TestRegistrySerializesConcurrentTurns(t *testing.T) {
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
	workdirs := workdir.NewManager(t.TempDir())
	ctx := context.Background()

	agent, err := agents.Create(ctx, store.CreateAgentInput{
		Name:  "mutex-agent",
		Model: "faux/test",
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.Create(ctx, store.CreateSessionInput{AgentID: agent.ID})
	if err != nil {
		t.Fatal(err)
	}

	gate := &gateHarness{delay: 40 * time.Millisecond}
	machine := &session.Machine{
		TenantID:  "default",
		SessionID: sess.ID,
		Sessions:  sessions,
		Events:    events,
		Pending:   pending,
		Hub:       hub,
		Workdirs:  workdirs,
		Harness:   gate,
	}
	reg := session.NewRegistry()
	reg.Register(sess.ID, machine)

	userEvent, _ := json.Marshal(map[string]any{
		"type": "user.message",
		"content": []map[string]string{
			{"type": "text", "text": "ping"},
		},
	})

	const turns = 4
	var enqueueWG sync.WaitGroup
	enqueueWG.Add(turns)
	var turnWG sync.WaitGroup
	turnWG.Add(turns)
	errCh := make(chan error, turns)
	for i := 0; i < turns; i++ {
		go func() {
			defer enqueueWG.Done()
			errCh <- reg.EnqueueUserMessage(ctx, sess.ID, userEvent, func(error) {
				turnWG.Done()
			})
		}()
	}
	enqueueWG.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("EnqueueUserMessage: %v", err)
		}
	}

	done := make(chan struct{})
	go func() {
		turnWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for queued turns")
	}
	if gate.peak.Load() != 1 {
		t.Fatalf("peak concurrent turns=%d, want 1", gate.peak.Load())
	}
}

func TestRegistrySerializesConcurrentAppends(t *testing.T) {
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
	workdirs := workdir.NewManager(t.TempDir())
	ctx := context.Background()

	agent, err := agents.Create(ctx, store.CreateAgentInput{
		Name:  "append-agent",
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
		Harness:   &harness.FakeClient{},
	}
	reg := session.NewRegistry()
	reg.Register(sess.ID, machine)

	const writers = 8
	var wg sync.WaitGroup
	wg.Add(writers)
	errCh := make(chan error, writers)
	for i := 0; i < writers; i++ {
		i := i
		go func() {
			defer wg.Done()
			payload, _ := json.Marshal(map[string]any{
				"type": "user.custom",
				"content": []map[string]string{
					{"type": "text", "text": "note"},
				},
				"index": i,
			})
			errCh <- reg.EnqueueEvents(
				ctx, sess.ID, []json.RawMessage{payload}, false, false, nil,
			)
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("EnqueueEvents: %v", err)
		}
	}

	list, err := events.ListEvents(ctx, sess.ID, 0, 100, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != writers {
		t.Fatalf("event count=%d, want %d", len(list), writers)
	}
	seen := make(map[int]bool, writers)
	for i, ev := range list {
		if ev.Seq != i+1 {
			t.Fatalf("seq=%d at index %d", ev.Seq, i)
		}
		var meta struct {
			Index int `json:"index"`
		}
		if err := json.Unmarshal(ev.Payload, &meta); err != nil {
			t.Fatal(err)
		}
		seen[meta.Index] = true
	}
	if len(seen) != writers {
		t.Fatalf("unique append indexes=%d, want %d", len(seen), writers)
	}
}
