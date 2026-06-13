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
		EvalRuns:  deps.EvalRuns,
		Sessions:  api.NewEvalSessionRunner(deps.Sessions),
		Evaluator: &harness.FakeClient{Text: "eval-ack"},
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

func TestEvalWorkerRubricPass(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	harnessClient := &harness.FakeClient{Text: "clear helpful answer"}
	deps, _ := testRouterDeps(t, db, harnessClient, "")
	handler := api.NewRouter(deps)
	events := store.NewEventRepo(db)
	worker := &eval.Worker{
		EvalRuns:  deps.EvalRuns,
		Sessions:  api.NewEvalSessionRunner(deps.Sessions),
		Events:    events,
		Agents:    deps.Agents,
		Models:    deps.ModelResolver,
		Evaluator: harnessClient,
	}
	ctx := context.Background()

	agentID := createEvalAgent(t, handler)
	envID := firstEnvironmentID(t, handler)
	runID := createEvalRun(t, handler, agentID, envID, map[string]any{
		"agent_id":       agentID,
		"environment_id": envID,
		"tasks": []map[string]any{
			{
				"id":       "rubric-pass",
				"messages": []string{"say hello"},
				"rubric": map[string]any{
					"description": "Respond helpfully",
					"criteria":    []string{"contains a greeting"},
				},
			},
		},
	})

	waitForEvalStatus(t, ctx, worker, deps.EvalRuns, runID, store.EvalStatusCompleted)

	req := httptest.NewRequest(http.MethodGet, "/v1/evals/runs/"+runID, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var detail map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &detail)
	trial0 := detail["tasks"].([]any)[0].(map[string]any)["trials"].([]any)[0].(map[string]any)
	if trial0["status"] != "completed" {
		t.Fatalf("trial status=%v want completed", trial0["status"])
	}
	if reward, ok := trial0["reward"].(float64); !ok || reward != 1 {
		t.Fatalf("trial reward=%v want 1", trial0["reward"])
	}
}

func TestEvalWorkerRubricFail(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	harnessClient := &harness.FakeClient{Text: "this answer will fail rubric"}
	deps, _ := testRouterDeps(t, db, harnessClient, "")
	handler := api.NewRouter(deps)
	events := store.NewEventRepo(db)
	worker := &eval.Worker{
		EvalRuns:  deps.EvalRuns,
		Sessions:  api.NewEvalSessionRunner(deps.Sessions),
		Events:    events,
		Agents:    deps.Agents,
		Models:    deps.ModelResolver,
		Evaluator: harnessClient,
	}
	ctx := context.Background()

	agentID := createEvalAgent(t, handler)
	envID := firstEnvironmentID(t, handler)
	runID := createEvalRun(t, handler, agentID, envID, map[string]any{
		"agent_id":       agentID,
		"environment_id": envID,
		"tasks": []map[string]any{
			{
				"id":       "rubric-fail",
				"messages": []string{"say hello"},
				"rubric": map[string]any{
					"description": "Respond helpfully",
					"criteria":    []string{"must not contain fail"},
				},
			},
		},
	})

	waitForEvalStatus(t, ctx, worker, deps.EvalRuns, runID, store.EvalStatusFailed)

	req := httptest.NewRequest(http.MethodGet, "/v1/evals/runs/"+runID, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var detail map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &detail)
	trial0 := detail["tasks"].([]any)[0].(map[string]any)["trials"].([]any)[0].(map[string]any)
	if trial0["status"] != "failed" {
		t.Fatalf("trial status=%v want failed", trial0["status"])
	}
	if reward, ok := trial0["reward"].(float64); !ok || reward != 0 {
		t.Fatalf("trial reward=%v want 0", trial0["reward"])
	}
	if trial0["error"] == nil || trial0["error"] == "" {
		t.Fatal("expected rubric failure error on trial")
	}
}

func createEvalAgent(t *testing.T, handler http.Handler) string {
	t.Helper()
	req := httptest.NewRequest(
		http.MethodPost, "/v1/agents",
		bytes.NewBufferString(`{"name":"eval-rubric-agent","model":"claude-sonnet-4-20250514"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("agent create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var agent map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &agent)
	return agent["id"].(string)
}

func firstEnvironmentID(t *testing.T, handler http.Handler) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/v1/environments?limit=5", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var envs map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &envs)
	return envs["data"].([]any)[0].(map[string]any)["id"].(string)
}

func createEvalRun(
	t *testing.T,
	handler http.Handler,
	_, _ string,
	body map[string]any,
) string {
	t.Helper()
	runBody, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/evals/runs", bytes.NewReader(runBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create eval status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	return created["run_id"].(string)
}

func waitForEvalStatus(
	t *testing.T,
	ctx context.Context,
	worker *eval.Worker,
	repo *store.EvalRunRepo,
	runID string,
	want store.EvalRunStatus,
) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := worker.Tick(ctx); err != nil {
			t.Fatal(err)
		}
		run, err := repo.Get(ctx, "default", runID)
		if err != nil {
			t.Fatal(err)
		}
		if run.Status == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	run, err := repo.Get(ctx, "default", runID)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("final status=%s want %s", run.Status, want)
}
