package store_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/open-ma/oma-building/internal/store"
)

func TestAppendEventSeqMonotonic(t *testing.T) {
	db := openTestDB(t)
	agents := store.NewAgentRepo(db.DB)
	environments := store.NewEnvironmentRepo(db.DB)
	ctx := context.Background()
	if err := environments.EnsureDefault(ctx); err != nil {
		t.Fatal(err)
	}
	sessions := store.NewSessionRepo(db.DB, agents, environments)
	events := store.NewEventRepo(db.DB)

	agent, err := agents.Create(ctx, store.CreateAgentInput{
		Name:  "evt-agent",
		Model: "claude-sonnet-4-20250514",
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.Create(ctx, store.CreateSessionInput{AgentID: agent.ID})
	if err != nil {
		t.Fatal(err)
	}

	raws := make([]json.RawMessage, 3)
	for i := range raws {
		raws[i], _ = json.Marshal(map[string]any{
			"type": "user.message",
			"content": []map[string]string{
				{"type": "text", "text": "hello"},
			},
		})
	}
	stored, err := events.AppendEvents(ctx, sess.ID, raws)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored) != 3 {
		t.Fatalf("len=%d", len(stored))
	}
	for i, ev := range stored {
		want := i + 1
		if ev.Seq != want {
			t.Fatalf("seq[%d]=%d", i, ev.Seq)
		}
	}
}
