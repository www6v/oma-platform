package store_test

import (
	"context"
	"testing"

	"github.com/open-ma/oma-building/internal/store"
)

func TestRecoverRunningSessions(t *testing.T) {
	db := openTestDB(t)
	agents := store.NewAgentRepo(db.DB)
	sessions := store.NewSessionRepo(db.DB, agents)
	ctx := context.Background()

	agent, err := agents.Create(ctx, store.CreateAgentInput{
		Name:  "recover",
		Model: "claude-sonnet-4-20250514",
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.Create(ctx, store.CreateSessionInput{AgentID: agent.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := sessions.BeginTurn(ctx, "default", sess.ID, "turn1"); err != nil {
		t.Fatal(err)
	}
	n, err := sessions.RecoverRunning(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("recovered=%d", n)
	}
	got, err := sessions.Get(ctx, "default", sess.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.SessionStatusInterrupted {
		t.Fatalf("status=%s", got.Status)
	}
}
