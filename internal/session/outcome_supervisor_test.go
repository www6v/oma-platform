package session

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/stream"
)

func TestPrepareDefineOutcomeMintsOutcomeID(t *testing.T) {
	raw := json.RawMessage(`{
		"type":"user.define_outcome",
		"description":"Must pass",
		"criteria":["one","two"]
	}`)
	out, err := PrepareDefineOutcome(raw)
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.Unmarshal(out, &body); err != nil {
		t.Fatal(err)
	}
	id, _ := body["outcome_id"].(string)
	if len(id) < 6 || id[:5] != "outc_" {
		t.Fatalf("outcome_id=%q want outc_ prefix", id)
	}
}

func TestPrepareDefineOutcomeRejectsEmpty(t *testing.T) {
	_, err := PrepareDefineOutcome(json.RawMessage(`{"type":"user.define_outcome"}`))
	if err == nil {
		t.Fatal("expected validation error")
	}
}

func TestRunOutcomeSupervisorGradeRevise(t *testing.T) {
	ctx := context.Background()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	agents := store.NewAgentRepo(db)
	environments := store.NewEnvironmentRepo(db)
	if err := environments.EnsureDefault(ctx); err != nil {
		t.Fatal(err)
	}
	sessions := store.NewSessionRepo(db, agents, environments)
	events := store.NewEventRepo(db)

	agent, err := agents.Create(ctx, store.CreateAgentInput{
		TenantID: "default",
		Name:     "outcome-test",
		Model:    "claude-sonnet-4-20250514",
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.Create(ctx, store.CreateSessionInput{
		TenantID: "default",
		AgentID:  agent.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	sim := &harness.FakeClient{Text: "attempt fail"}
	machine := &Machine{
		TenantID:         "default",
		SessionID:        sess.ID,
		Sessions:         sessions,
		Agents:           agents,
		Events:           events,
		Hub:              stream.NewHub(),
		Harness:          sim,
		OutcomeEvaluator: sim,
	}

	outcomeID := "outc_test1234567890"
	defineRaw, _ := json.Marshal(map[string]any{
		"type":        "user.define_outcome",
		"outcome_id":  outcomeID,
		"description": "No fail substring",
	})
	if err := machine.ActivateOutcomeFromEvent(ctx, defineRaw); err != nil {
		t.Fatal(err)
	}
	if _, err := events.AppendEvents(ctx, sess.ID, []json.RawMessage{
		defineRaw,
		json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"go"}]}`),
		json.RawMessage(`{
			"type":"agent.message",
			"id":"ag_1",
			"content":[{"type":"text","text":"attempt fail"}]
		}`),
	}); err != nil {
		t.Fatal(err)
	}

	turns := 0
	if err := RunOutcomeSupervisor(ctx, OutcomeSupervisorDeps{
		Machine:   machine,
		Evaluator: sim,
		RunHarnessTurn: func(runCtx context.Context) error {
			turns++
			sim.Text = "revision ok"
			msg, _ := json.Marshal(map[string]any{
				"type":    "agent.message",
				"id":      "ag_rev",
				"content": []map[string]string{{"type": "text", "text": sim.Text}},
			})
			return machine.publishEvents(runCtx, []json.RawMessage{msg})
		},
	}); err != nil {
		t.Fatal(err)
	}
	if turns != 1 {
		t.Fatalf("revision turns=%d want 1", turns)
	}

	state, err := machine.readOutcomeState(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if state.Outcome != nil {
		t.Fatal("expected active outcome cleared after satisfied")
	}
	if len(state.OutcomeEvaluations) < 2 {
		t.Fatalf("evaluations=%d want >=2", len(state.OutcomeEvaluations))
	}
	last := state.OutcomeEvaluations[len(state.OutcomeEvaluations)-1]
	if last.Result != "satisfied" {
		t.Fatalf("terminal result=%q want satisfied", last.Result)
	}
}
