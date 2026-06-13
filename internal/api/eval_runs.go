package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/store"
)

type evalRunsDeps struct {
	EvalRuns     *store.EvalRunRepo
	Agents       *store.AgentRepo
	Environments *store.EnvironmentRepo
}

func mountEvalRunRoutes(r chi.Router, deps evalRunsDeps) {
	if deps.EvalRuns == nil {
		return
	}

	r.Route("/v1/evals", func(r chi.Router) {
		r.Post("/runs", func(w http.ResponseWriter, req *http.Request) {
			var body struct {
				AgentID       string            `json:"agent_id"`
				EnvironmentID string            `json:"environment_id"`
				Tasks         []evalTaskSpec    `json:"tasks"`
			}
			if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
				writeError(w, http.StatusBadRequest, "invalid json")
				return
			}
			if body.AgentID == "" {
				writeError(w, http.StatusBadRequest, "agent_id is required")
				return
			}
			if body.EnvironmentID == "" {
				writeError(w, http.StatusBadRequest, "environment_id is required")
				return
			}
			if len(body.Tasks) == 0 {
				writeError(
					w, http.StatusBadRequest,
					"tasks array is required and must be non-empty",
				)
				return
			}
			for _, task := range body.Tasks {
				if task.ID == "" {
					writeError(w, http.StatusBadRequest, "task missing id")
					return
				}
				if len(task.Messages) == 0 {
					writeError(
						w, http.StatusBadRequest,
						"task "+task.ID+" requires non-empty messages array",
					)
					return
				}
			}

			tid := tenantID(req)
			agent, err := deps.Agents.Get(req.Context(), tid, body.AgentID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if agent == nil {
				writeError(w, http.StatusNotFound, "Agent not found")
				return
			}
			if deps.Environments != nil {
				env, err := deps.Environments.Get(
					req.Context(), tid, body.EnvironmentID,
				)
				if err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				if env == nil {
					writeError(w, http.StatusNotFound, "Environment not found")
					return
				}
			}

			initialResults := buildInitialEvalResults(body.Tasks)
			payload, err := json.Marshal(initialResults)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			run, err := deps.EvalRuns.Create(req.Context(), store.CreateEvalRunInput{
				TenantID:      tid,
				AgentID:       body.AgentID,
				EnvironmentID: body.EnvironmentID,
				Status:        store.EvalStatusPending,
				Results:       payload,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"run_id":      run.ID,
				"task_count":  len(body.Tasks),
			})
		})

		r.Get("/runs", func(w http.ResponseWriter, req *http.Request) {
			limit := 100
			if raw := req.URL.Query().Get("limit"); raw != "" {
				parsed, err := strconv.Atoi(raw)
				if err != nil || parsed < 1 {
					limit = 100
				} else {
					limit = parsed
				}
				if limit > 1000 {
					limit = 1000
				}
			}
			var status store.EvalRunStatus
			if raw := req.URL.Query().Get("status"); raw != "" {
				switch raw {
				case "pending", "running", "completed", "failed":
					status = store.EvalRunStatus(raw)
				default:
					writeJSON(w, http.StatusBadRequest, map[string]any{
						"error": map[string]any{
							"type":    "invalid_request_error",
							"code":    "invalid_status",
							"message": "Invalid status '" + raw + "'; expected one of pending|running|completed|failed.",
						},
					})
					return
				}
			}
			runs, err := deps.EvalRuns.List(req.Context(), tenantID(req), store.EvalRunListOptions{
				Limit:         limit,
				AgentID:       req.URL.Query().Get("agent_id"),
				EnvironmentID: req.URL.Query().Get("environment_id"),
				Status:        status,
			})
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			data := make([]map[string]any, 0, len(runs))
			for i := range runs {
				data = append(data, serializeEvalRun(&runs[i]))
			}
			writeJSON(w, http.StatusOK, map[string]any{"data": data})
		})

		r.Route("/runs/{id}", func(r chi.Router) {
			r.Get("/", func(w http.ResponseWriter, req *http.Request) {
				run, err := deps.EvalRuns.Get(
					req.Context(), tenantID(req), chi.URLParam(req, "id"),
				)
				if err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				if run == nil {
					writeError(w, http.StatusNotFound, "Run not found")
					return
				}
				writeJSON(w, http.StatusOK, serializeEvalRun(run))
			})

			r.Delete("/", func(w http.ResponseWriter, req *http.Request) {
				tid := tenantID(req)
				id := chi.URLParam(req, "id")
				run, err := deps.EvalRuns.Get(req.Context(), tid, id)
				if err != nil {
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				if run == nil {
					writeError(w, http.StatusNotFound, "Run not found")
					return
				}
				if run.Status == store.EvalStatusPending ||
					run.Status == store.EvalStatusRunning {
					if err := deps.EvalRuns.MarkFailed(
						req.Context(), tid, id, "cancelled by user",
					); err != nil {
						writeError(w, http.StatusInternalServerError, err.Error())
						return
					}
				}
				if err := deps.EvalRuns.Delete(req.Context(), tid, id); err != nil {
					if errors.Is(err, store.ErrEvalRunNotFound) {
						writeError(w, http.StatusNotFound, "Run not found")
						return
					}
					writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				writeJSON(w, http.StatusOK, map[string]any{
					"type": "eval_run_deleted",
					"id":   id,
				})
			})
		})
	})
}

type evalTaskSpec struct {
	ID          string           `json:"id"`
	Messages    []string         `json:"messages"`
	Trials      int              `json:"trials,omitempty"`
	TimeoutMs   int              `json:"timeout_ms,omitempty"`
	SetupScript string           `json:"setup_script,omitempty"`
	Rubric      *evalRubricSpec  `json:"rubric,omitempty"`
}

type evalRubricSpec struct {
	Description string   `json:"description"`
	Criteria    []string `json:"criteria,omitempty"`
}

func buildInitialEvalResults(tasks []evalTaskSpec) map[string]any {
	items := make([]map[string]any, 0, len(tasks))
	for _, spec := range tasks {
		trialCount := spec.Trials
		if trialCount < 1 {
			trialCount = 1
		}
		trials := make([]map[string]any, 0, trialCount)
		for i := 0; i < trialCount; i++ {
			trials = append(trials, map[string]any{
				"trial_index": i,
				"status":      "pending",
			})
		}
		items = append(items, map[string]any{
			"id":          spec.ID,
			"spec":        spec,
			"status":      "pending",
			"trials":      trials,
			"trial_total": trialCount,
		})
	}
	return map[string]any{
		"task_count":      len(tasks),
		"completed_count": 0,
		"failed_count":    0,
		"tasks":           items,
	}
}

func serializeEvalRun(run *store.EvalRunRow) map[string]any {
	partial := map[string]any{}
	if len(run.Results) > 0 {
		_ = json.Unmarshal(run.Results, &partial)
	}
	taskCount := asInt(partial["task_count"])
	completed := asInt(partial["completed_count"])
	failed := asInt(partial["failed_count"])
	tasks, _ := partial["tasks"].([]any)
	if tasks == nil {
		tasks = []any{}
	}
	out := map[string]any{
		"id":               run.ID,
		"tenant_id":        run.TenantID,
		"agent_id":         run.AgentID,
		"environment_id":   run.EnvironmentID,
		"status":           string(run.Status),
		"created_at":       msToISO(run.StartedAt),
		"started_at":       msToISO(run.StartedAt),
		"task_count":       taskCount,
		"completed_count":  completed,
		"failed_count":     failed,
		"tasks":            tasks,
	}
	if run.CompletedAt.Valid {
		out["ended_at"] = msToISO(run.CompletedAt.Int64)
	}
	if run.Error.Valid {
		out["error"] = run.Error.String
	}
	return out
}

func asInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return 0
	}
}
