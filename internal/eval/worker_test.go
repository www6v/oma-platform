package eval_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/open-ma/oma-building/internal/eval"
	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/modelresolve"
	"github.com/open-ma/oma-building/internal/store"
)

type fakeSessions struct {
	status map[string]string
}

func (f *fakeSessions) CreateTaskSession(
	_ context.Context,
	_, _, _, _ string,
) (string, error) {
	id := "sess-eval-fake"
	if f.status == nil {
		f.status = map[string]string{}
	}
	f.status[id] = "running"
	return id, nil
}

func (f *fakeSessions) SendUserMessage(_ context.Context, sessionID, _ string) error {
	if f.status == nil {
		f.status = map[string]string{}
	}
	f.status[sessionID] = "running"
	return nil
}

func (f *fakeSessions) SessionStatus(
	_ context.Context,
	_, sessionID string,
) (string, error) {
	if f.status == nil {
		return "idle", nil
	}
	if st, ok := f.status[sessionID]; ok {
		return st, nil
	}
	return "idle", nil
}

func TestWorkerPendingToCompleted(t *testing.T) {
	ctx := context.Background()
	db := store.OpenTestDB(t)
	repo := store.NewEvalRunRepo(db.DB)

	initial, _ := json.Marshal(map[string]any{
		"task_count":      1,
		"completed_count": 0,
		"failed_count":    0,
		"tasks": []map[string]any{
			{
				"id": "single",
				"spec": map[string]any{
					"id":       "single",
					"messages": []string{"hello"},
				},
				"status":      "pending",
				"trial_total": 1,
				"trials": []map[string]any{
					{"trial_index": 0, "status": "pending"},
				},
			},
		},
	})
	run, err := repo.Create(ctx, store.CreateEvalRunInput{
		TenantID:      "default",
		AgentID:       "agent-1",
		EnvironmentID: "env-1",
		Status:        store.EvalStatusPending,
		Results:       initial,
	})
	if err != nil {
		t.Fatal(err)
	}

	sessions := &fakeSessions{status: map[string]string{}}
	worker := &eval.Worker{
		EvalRuns:  repo,
		Sessions:  sessions,
		Evaluator: &harness.FakeClient{},
	}

	if _, err := worker.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := repo.Get(ctx, "default", run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.EvalStatusRunning {
		t.Fatalf("after tick1 status=%s want running", got.Status)
	}

	sessions.status["sess-eval-fake"] = "idle"
	if _, err := worker.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	got, err = repo.Get(ctx, "default", run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.EvalStatusCompleted {
		t.Fatalf("after tick2 status=%s want completed", got.Status)
	}
	if !got.CompletedAt.Valid {
		t.Fatal("expected completed_at")
	}
}

func appendAgentMessage(
	t *testing.T,
	ctx context.Context,
	events *store.EventRepo,
	sessionID, text string,
) {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"type": "agent.message",
		"id":   "evt-rubric-test",
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := events.AppendEvents(ctx, sessionID, []json.RawMessage{payload}); err != nil {
		t.Fatal(err)
	}
}

func createRubricEvalRun(
	t *testing.T,
	ctx context.Context,
	repo *store.EvalRunRepo,
	agentID string,
) *store.EvalRunRow {
	t.Helper()
	initial, err := json.Marshal(map[string]any{
		"task_count":      1,
		"completed_count": 0,
		"failed_count":    0,
		"tasks": []map[string]any{
			{
				"id": "rubric-task",
				"spec": map[string]any{
					"id":       "rubric-task",
					"messages": []string{"complete the task"},
					"rubric": map[string]any{
						"description": "Answer clearly",
						"criteria":    []string{"mentions the expected outcome"},
					},
				},
				"status":      "pending",
				"trial_total": 1,
				"trials": []map[string]any{
					{"trial_index": 0, "status": "pending"},
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	run, err := repo.Create(ctx, store.CreateEvalRunInput{
		TenantID:      "default",
		AgentID:       agentID,
		EnvironmentID: "env-1",
		Status:        store.EvalStatusPending,
		Results:       initial,
	})
	if err != nil {
		t.Fatal(err)
	}
	return run
}

func trialFromResults(t *testing.T, results json.RawMessage) map[string]any {
	t.Helper()
	var partial map[string]any
	if err := json.Unmarshal(results, &partial); err != nil {
		t.Fatal(err)
	}
	tasks, ok := partial["tasks"].([]any)
	if !ok || len(tasks) == 0 {
		t.Fatal("expected tasks in results")
	}
	task0, ok := tasks[0].(map[string]any)
	if !ok {
		t.Fatal("expected task object")
	}
	trials, ok := task0["trials"].([]any)
	if !ok || len(trials) == 0 {
		t.Fatal("expected trials in task")
	}
	trial0, ok := trials[0].(map[string]any)
	if !ok {
		t.Fatal("expected trial object")
	}
	return trial0
}

func TestWorkerRubricPass(t *testing.T) {
	ctx := context.Background()
	db := store.OpenTestDB(t)
	repo := store.NewEvalRunRepo(db.DB)
	agents := store.NewAgentRepo(db.DB)
	events := store.NewEventRepo(db.DB)

	agent, err := agents.Create(ctx, store.CreateAgentInput{
		TenantID: "default",
		Name:     "rubric-agent",
		Model:    "claude-test",
	})
	if err != nil {
		t.Fatal(err)
	}

	run := createRubricEvalRun(t, ctx, repo, agent.ID)
	sessions := &fakeSessions{status: map[string]string{}}
	worker := &eval.Worker{
		EvalRuns:  repo,
		Sessions:  sessions,
		Events:    events,
		Agents:    agents,
		Models:    &modelresolve.Resolver{},
		Evaluator: &harness.FakeClient{},
	}

	if _, err := worker.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	appendAgentMessage(t, ctx, events, "sess-eval-fake", "expected outcome delivered")
	sessions.status["sess-eval-fake"] = "idle"

	if _, err := worker.Tick(ctx); err != nil {
		t.Fatal(err)
	}

	got, err := repo.Get(ctx, "default", run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.EvalStatusCompleted {
		t.Fatalf("status=%s want completed", got.Status)
	}
	trial := trialFromResults(t, got.Results)
	if trial["status"] != "completed" {
		t.Fatalf("trial status=%v want completed", trial["status"])
	}
	if reward, _ := trial["reward"].(float64); reward != 1 {
		t.Fatalf("trial reward=%v want 1", trial["reward"])
	}
}

func TestWorkerRubricFail(t *testing.T) {
	ctx := context.Background()
	db := store.OpenTestDB(t)
	repo := store.NewEvalRunRepo(db.DB)
	agents := store.NewAgentRepo(db.DB)
	events := store.NewEventRepo(db.DB)

	agent, err := agents.Create(ctx, store.CreateAgentInput{
		TenantID: "default",
		Name:     "rubric-agent",
		Model:    "claude-test",
	})
	if err != nil {
		t.Fatal(err)
	}

	run := createRubricEvalRun(t, ctx, repo, agent.ID)
	sessions := &fakeSessions{status: map[string]string{}}
	worker := &eval.Worker{
		EvalRuns:  repo,
		Sessions:  sessions,
		Events:    events,
		Agents:    agents,
		Models:    &modelresolve.Resolver{},
		Evaluator: &harness.FakeClient{},
	}

	if _, err := worker.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	appendAgentMessage(t, ctx, events, "sess-eval-fake", "this output will fail rubric")
	sessions.status["sess-eval-fake"] = "idle"

	if _, err := worker.Tick(ctx); err != nil {
		t.Fatal(err)
	}

	got, err := repo.Get(ctx, "default", run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.EvalStatusFailed {
		t.Fatalf("status=%s want failed", got.Status)
	}
	trial := trialFromResults(t, got.Results)
	if trial["status"] != "failed" {
		t.Fatalf("trial status=%v want failed", trial["status"])
	}
	if reward, _ := trial["reward"].(float64); reward != 0 {
		t.Fatalf("trial reward=%v want 0", trial["reward"])
	}
	if trial["error"] == nil || trial["error"] == "" {
		t.Fatal("expected trial error from rubric feedback")
	}
}

func TestWorkerRubricFailsWithoutAgentOutput(t *testing.T) {
	ctx := context.Background()
	db := store.OpenTestDB(t)
	repo := store.NewEvalRunRepo(db.DB)
	agents := store.NewAgentRepo(db.DB)
	events := store.NewEventRepo(db.DB)

	agent, err := agents.Create(ctx, store.CreateAgentInput{
		TenantID: "default",
		Name:     "rubric-agent",
		Model:    "claude-test",
	})
	if err != nil {
		t.Fatal(err)
	}

	run := createRubricEvalRun(t, ctx, repo, agent.ID)
	sessions := &fakeSessions{status: map[string]string{}}
	worker := &eval.Worker{
		EvalRuns:  repo,
		Sessions:  sessions,
		Events:    events,
		Agents:    agents,
		Models:    &modelresolve.Resolver{},
		Evaluator: &harness.FakeClient{},
	}

	if _, err := worker.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	sessions.status["sess-eval-fake"] = "idle"

	if _, err := worker.Tick(ctx); err != nil {
		t.Fatal(err)
	}

	got, err := repo.Get(ctx, "default", run.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.EvalStatusFailed {
		t.Fatalf("status=%s want failed", got.Status)
	}
	trial := trialFromResults(t, got.Results)
	if trial["status"] != "failed" {
		t.Fatalf("trial status=%v want failed", trial["status"])
	}
	if trial["error"] != "no agent output to evaluate" {
		t.Fatalf("trial error=%v", trial["error"])
	}
}
