package eval_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/open-ma/oma-building/internal/eval"
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
	worker := &eval.Worker{EvalRuns: repo, Sessions: sessions}

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
