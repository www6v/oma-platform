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

type threadCaptureHarness struct {
	harness.FakeClient
	mu        sync.Mutex
	threadIDs []string
}

func (c *threadCaptureHarness) RunTurnStream(
	ctx context.Context,
	req harness.TurnRequest,
	onEvent harness.EventHandler,
) error {
	c.mu.Lock()
	c.threadIDs = append(c.threadIDs, req.SessionThreadID)
	c.mu.Unlock()
	return c.FakeClient.RunTurnStream(ctx, req, onEvent)
}

func (c *threadCaptureHarness) lastThreadID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.threadIDs) == 0 {
		return ""
	}
	return c.threadIDs[len(c.threadIDs)-1]
}

func TestEnqueueEventsRunsTurnOnTeammateThread(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	if err := store.Migrate(db); err != nil {
		t.Fatal(err)
	}

	agents := store.NewAgentRepo(db)
	environments := store.NewEnvironmentRepo(db)
	if err := environments.EnsureDefault(context.Background()); err != nil {
		t.Fatal(err)
	}
	sessions := store.NewSessionRepo(db, agents, environments)
	events := store.NewEventRepo(db)
	pending := store.NewPendingRepo(db)
	teams := store.NewTeamRepo(db)
	hub := stream.NewHub()
	workdirs := workdir.NewManager(t.TempDir(), "")
	ctx := context.Background()

	lead, err := agents.Create(ctx, store.CreateAgentInput{
		Name:  "lead-agent",
		Model: "faux/test",
	})
	if err != nil {
		t.Fatal(err)
	}
	worker, err := agents.Create(ctx, store.CreateAgentInput{
		Name:  "coder-agent",
		Model: "faux/test",
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.Create(ctx, store.CreateSessionInput{
		AgentID: lead.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UnixMilli()
	if err := teams.CreateTeam(ctx, store.Team{
		ID:           "team-1",
		SessionID:    sess.ID,
		TenantID:     "default",
		Name:         "alpha",
		LeadThreadID: "sthr_primary",
		LeadAgentID:  lead.ID,
		Status:       "active",
		CreatedAt:    now,
	}); err != nil {
		t.Fatal(err)
	}
	if err := teams.CreateMember(ctx, store.TeamMember{
		ID:          "tmem-coder",
		TeamID:      "team-1",
		AgentID:     worker.ID,
		DisplayName: "coder",
		ThreadID:    "sthr_coder",
		BackendType: "in_process",
		Status:      "idle",
		JoinedAt:    now,
	}); err != nil {
		t.Fatal(err)
	}

	capture := &threadCaptureHarness{}
	machine := &session.Machine{
		TenantID:  "default",
		SessionID: sess.ID,
		Sessions:  sessions,
		Agents:    agents,
		Teams:     teams,
		Events:    events,
		Pending:   pending,
		Hub:       hub,
		Workdirs:  workdirs,
		Harness:   capture,
	}
	reg := session.NewRegistry()
	reg.Register(sess.ID, machine)

	teammateMsg, _ := json.Marshal(map[string]any{
		"type":              "user.message",
		"session_thread_id": "sthr_coder",
		"content": []map[string]string{
			{"type": "text", "text": "write quicksort"},
		},
	})
	if err := reg.EnqueueEvents(
		ctx, sess.ID, []json.RawMessage{teammateMsg}, true, false, nil,
	); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if capture.lastThreadID() == "sthr_coder" {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("expected teammate thread turn, got %q", capture.lastThreadID())
}
