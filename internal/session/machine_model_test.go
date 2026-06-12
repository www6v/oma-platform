package session_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/modelresolve"
	"github.com/open-ma/oma-building/internal/session"
	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/stream"
	"github.com/open-ma/oma-building/internal/workdir"
)

func TestRunTurnResolvesModelCardByModelID(t *testing.T) {
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
	cards := store.NewModelCardRepo(db)
	if _, err := cards.Create(ctx, store.CreateModelCardInput{
		ModelID:  "claude-prod",
		Model:    "claude-sonnet-4-20250514",
		Provider: "ant",
		APIKey:   "sk-test-9999",
	}); err != nil {
		t.Fatal(err)
	}

	sessions := store.NewSessionRepo(db, agents, environments)
	events := store.NewEventRepo(db)
	recording := &harness.RecordingClient{FakeClient: harness.FakeClient{Text: "ok"}}

	agent, err := agents.Create(ctx, store.CreateAgentInput{
		Name:  "card-agent",
		Model: "claude-prod",
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
		Harness:   recording,
		Models:    &modelresolve.Resolver{Cards: cards},
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

	turnReq, ok := recording.LastRequest()
	if !ok {
		t.Fatal("expected recorded turn request")
	}
	if turnReq.Agent.Model != "claude-prod" {
		t.Fatalf("agent model=%q", turnReq.Agent.Model)
	}
	if turnReq.Model.Model != "claude-sonnet-4-20250514" {
		t.Fatalf("wire model=%q", turnReq.Model.Model)
	}
	if turnReq.Model.APIKey != "sk-test-9999" {
		t.Fatalf("api_key=%q", turnReq.Model.APIKey)
	}
}

func TestRunTurnUsesDefaultCardForProviderModel(t *testing.T) {
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
	cards := store.NewModelCardRepo(db)
	if _, err := cards.Create(ctx, store.CreateModelCardInput{
		ModelID:     "default-card",
		Provider:    "ant",
		APIKey:      "sk-default-key",
		MakeDefault: true,
	}); err != nil {
		t.Fatal(err)
	}

	sessions := store.NewSessionRepo(db, agents, environments)
	events := store.NewEventRepo(db)
	recording := &harness.RecordingClient{}

	agent, err := agents.Create(ctx, store.CreateAgentInput{
		Name:  "raw-model-agent",
		Model: "claude-sonnet-4-20250514",
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
		Harness:   recording,
		Models:    &modelresolve.Resolver{Cards: cards},
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

	turnReq, ok := recording.LastRequest()
	if !ok {
		t.Fatal("expected recorded turn request")
	}
	if turnReq.Model.Model != "claude-sonnet-4-20250514" {
		t.Fatalf("wire model=%q", turnReq.Model.Model)
	}
	if turnReq.Model.Provider != "ant" {
		t.Fatalf("provider=%q", turnReq.Model.Provider)
	}
	if turnReq.Model.APIKey != "sk-default-key" {
		t.Fatalf("api_key=%q", turnReq.Model.APIKey)
	}
}
