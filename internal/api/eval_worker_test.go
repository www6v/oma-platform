package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/api"
	"github.com/open-ma/oma-building/internal/eval"
	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/store"
)

func TestEvalWorkerAdvancesRun(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	deps, _ := testRouterDeps(t, db, &harness.FakeClient{Text: "eval-ack"}, "")
	handler := api.NewRouter(deps)
	worker := &eval.Worker{
		EvalRuns: deps.EvalRuns,
		Sessions: api.NewEvalSessionRunner(deps.Sessions),
	}
	ctx := context.Background()

	createAgent := `{"name":"eval-agent","model":"claude-sonnet-4-20250514"}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/agents",
		bytes.NewBufferString(createAgent),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("agent create status=%d", rec.Code)
	}
	var agent map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &agent)
	agentID := agent["id"].(string)

	req = httptest.NewRequest(http.MethodGet, "/v1/environments?limit=5", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var envs map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &envs)
	envID := envs["data"].([]any)[0].(map[string]any)["id"].(string)

	runBody, _ := json.Marshal(map[string]any{
		"agent_id":       agentID,
		"environment_id": envID,
		"tasks": []map[string]any{
			{"id": "single", "messages": []string{"hello world"}},
		},
	})
	req = httptest.NewRequest(
		http.MethodPost, "/v1/evals/runs", bytes.NewReader(runBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create eval status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	runID := created["run_id"].(string)

	run, err := deps.EvalRuns.Get(ctx, "default", runID)
	if err != nil || run == nil {
		t.Fatal(err)
	}
	if run.Status != store.EvalStatusPending {
		t.Fatalf("expected pending, got %s", run.Status)
	}

	if _, err := worker.Tick(ctx); err != nil {
		t.Fatal(err)
	}
	run, err = deps.EvalRuns.Get(ctx, "default", runID)
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != store.EvalStatusRunning {
		t.Fatalf("after tick1 status=%s want running", run.Status)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := worker.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		run, err = deps.EvalRuns.Get(ctx, "default", runID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status == store.EvalStatusCompleted {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if run.Status != store.EvalStatusCompleted {
		t.Fatalf("final status=%s want completed", run.Status)
	}

	req = httptest.NewRequest(
		http.MethodGet, "/v1/evals/runs/"+runID, nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var detail map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &detail)
	if detail["status"] != "completed" {
		t.Fatalf("api status=%v", detail["status"])
	}
	tasks := detail["tasks"].([]any)
	task0 := tasks[0].(map[string]any)
	if task0["status"] != "completed" {
		t.Fatalf("task status=%v", task0["status"])
	}
	trials := task0["trials"].([]any)
	trial0 := trials[0].(map[string]any)
	if trial0["trajectory_id"] == nil {
		t.Fatal("expected trajectory_id")
	}
	trajID := trial0["trajectory_id"].(string)
	if len(trajID) < 3 || trajID[:3] != "tr-" {
		t.Fatalf("trajectory_id=%q", trajID)
	}
}
