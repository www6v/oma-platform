package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/modelresolve"
	"github.com/open-ma/oma-building/internal/store"
)

const defaultTrialTimeout = time.Hour

// Worker advances pending/running eval runs toward completion.
type Worker struct {
	EvalRuns  *store.EvalRunRepo
	Sessions  SessionRunner
	Events    *store.EventRepo
	Agents    *store.AgentRepo
	Models    *modelresolve.Resolver
	Evaluator harness.OutcomeEvaluator
}

// TickResult summarizes one worker pass.
type TickResult struct {
	Advanced int
	Total    int
}

// Tick scans active runs and advances each one step.
func (w *Worker) Tick(ctx context.Context) (TickResult, error) {
	if w == nil || w.EvalRuns == nil || w.Sessions == nil {
		return TickResult{}, nil
	}
	active, err := w.EvalRuns.ListActive(ctx)
	if err != nil {
		return TickResult{}, err
	}
	result := TickResult{Total: len(active)}
	for i := range active {
		if err := w.advanceRun(ctx, &active[i]); err != nil {
			_ = w.failRun(ctx, &active[i], err)
		}
		result.Advanced++
	}
	return result, nil
}

func (w *Worker) advanceRun(ctx context.Context, row *store.EvalRunRow) error {
	state, err := parseRunState(row)
	if err != nil {
		return err
	}
	if state.Status == "completed" || state.Status == "failed" {
		return nil
	}
	if state.Status == "pending" {
		state.Status = "running"
	}

	progressed := false
	for i := range state.Tasks {
		task := &state.Tasks[i]
		if task.Status != "pending" && task.Status != "running" {
			continue
		}
		if w.advanceTask(ctx, row, task) {
			progressed = true
		}
	}

	state.CompletedCount = 0
	state.FailedCount = 0
	for i := range state.Tasks {
		switch state.Tasks[i].Status {
		case "completed":
			state.CompletedCount++
		case "failed":
			state.FailedCount++
		}
	}

	done := state.CompletedCount+state.FailedCount == state.TaskCount
	if done {
		status := store.EvalStatusCompleted
		if state.FailedCount > 0 && state.CompletedCount == 0 {
			status = store.EvalStatusFailed
		}
		var score *float64
		if state.CompletedCount > 0 {
			avg := float64(state.CompletedCount) / float64(state.TaskCount)
			score = &avg
		}
		payload, err := marshalRunState(state)
		if err != nil {
			return err
		}
		return w.EvalRuns.MarkFinished(
			ctx, row.TenantID, row.ID, status, payload, score, "",
		)
	}
	if !progressed {
		return nil
	}
	payload, err := marshalRunState(state)
	if err != nil {
		return err
	}
	return w.EvalRuns.UpdateProgress(
		ctx, row.TenantID, row.ID, store.EvalStatusRunning, payload,
	)
}

func (w *Worker) advanceTask(
	ctx context.Context,
	row *store.EvalRunRow,
	task *taskState,
) bool {
	if task.Status == "completed" || task.Status == "failed" {
		return false
	}
	progressed := false
	for i := range task.Trials {
		trial := &task.Trials[i]
		if trial.Status == "completed" || trial.Status == "failed" {
			continue
		}
		if w.advanceTrial(ctx, row, task, trial) {
			progressed = true
		}
	}

	pass := 0
	for i := range task.Trials {
		switch task.Trials[i].Status {
		case "completed":
			pass++
		case "failed":
			// counted below
		}
	}
	task.TrialPassCount = pass
	task.TrialTotal = len(task.Trials)
	failed := 0
	for i := range task.Trials {
		if task.Trials[i].Status == "failed" {
			failed++
		}
	}
	if pass+failed == len(task.Trials) {
		if pass == len(task.Trials) {
			task.Status = "completed"
		} else {
			task.Status = "failed"
			for i := range task.Trials {
				if task.Trials[i].Error != "" {
					task.Error = task.Trials[i].Error
					break
				}
			}
		}
	} else {
		task.Status = "running"
	}
	return progressed
}

func (w *Worker) advanceTrial(
	ctx context.Context,
	row *store.EvalRunRow,
	task *taskState,
	trial *trialState,
) bool {
	if trial.Status == "completed" || trial.Status == "failed" {
		return false
	}

	messages := task.Spec.Messages
	if len(messages) == 0 {
		trial.Status = "failed"
		trial.Error = "task has no messages"
		trial.EndedAt = nowISO()
		return true
	}

	if trial.Status == "pending" {
		title := fmt.Sprintf("eval %s :: %s", row.ID, task.ID)
		sessionID, err := w.Sessions.CreateTaskSession(
			ctx, row.TenantID, row.AgentID, row.EnvironmentID, title,
		)
		if err != nil {
			trial.Status = "failed"
			trial.Error = err.Error()
			trial.EndedAt = nowISO()
			return true
		}
		trial.SessionID = sessionID
		trial.Status = "running"
		trial.StartedAt = nowISO()
		trial.CurrentMessageIndex = 0
		if err := w.Sessions.SendUserMessage(ctx, sessionID, messages[0]); err != nil {
			trial.Status = "failed"
			trial.Error = err.Error()
			trial.EndedAt = nowISO()
			return true
		}
		return true
	}

	if trial.SessionID == "" {
		trial.Status = "failed"
		trial.Error = "running trial missing session_id"
		trial.EndedAt = nowISO()
		return true
	}

	timeout := defaultTrialTimeout
	if task.Spec.TimeoutMs > 0 {
		timeout = time.Duration(task.Spec.TimeoutMs) * time.Millisecond
	}
	if trial.StartedAt != "" {
		if started, err := time.Parse(time.RFC3339Nano, trial.StartedAt); err == nil {
			if time.Since(started) > timeout {
				trial.Status = "failed"
				trial.Error = fmt.Sprintf(
					"trial timeout after %s", timeout.Round(time.Second),
				)
				trial.EndedAt = nowISO()
				return true
			}
		}
	}

	status, err := w.Sessions.SessionStatus(ctx, row.TenantID, trial.SessionID)
	if err != nil || status != string(store.SessionStatusIdle) {
		return false
	}

	nextIndex := trial.CurrentMessageIndex + 1
	if nextIndex < len(messages) {
		if err := w.Sessions.SendUserMessage(
			ctx, trial.SessionID, messages[nextIndex],
		); err != nil {
			trial.Status = "failed"
			trial.Error = err.Error()
			trial.EndedAt = nowISO()
			return true
		}
		trial.CurrentMessageIndex = nextIndex
		return true
	}

	trial.TrajectoryID = "tr-" + trial.SessionID
	reward, feedback, scoreErr := w.scoreTrial(ctx, row, task, trial)
	if scoreErr != nil {
		trial.Status = "failed"
		trial.Error = scoreErr.Error()
		trial.EndedAt = nowISO()
		return true
	}
	trial.Reward = reward
	if reward < 1 {
		trial.Status = "failed"
		if feedback != "" {
			trial.Error = feedback
		} else {
			trial.Error = "rubric not satisfied"
		}
		trial.EndedAt = nowISO()
		return true
	}
	trial.Status = "completed"
	trial.EndedAt = nowISO()
	return true
}

func (w *Worker) failRun(
	ctx context.Context,
	row *store.EvalRunRow,
	runErr error,
) error {
	msg := runErr.Error()
	return w.EvalRuns.MarkFinished(
		ctx, row.TenantID, row.ID,
		store.EvalStatusFailed, row.Results, nil, msg,
	)
}

type runState struct {
	Status         string
	TaskCount      int
	CompletedCount int
	FailedCount    int
	Tasks          []taskState
}

type taskState struct {
	ID             string
	Spec           taskSpec
	Status         string
	Trials         []trialState
	TrialTotal     int
	TrialPassCount int
	Error          string
}

type taskSpec struct {
	ID        string       `json:"id"`
	Messages  []string     `json:"messages"`
	Rubric    *rubricSpec  `json:"rubric,omitempty"`
	TimeoutMs int          `json:"timeout_ms,omitempty"`
}

type trialState struct {
	TrialIndex          int     `json:"trial_index"`
	Status              string  `json:"status"`
	SessionID           string  `json:"session_id,omitempty"`
	CurrentMessageIndex int     `json:"current_message_index,omitempty"`
	StartedAt           string  `json:"started_at,omitempty"`
	EndedAt             string  `json:"ended_at,omitempty"`
	TrajectoryID        string  `json:"trajectory_id,omitempty"`
	Reward              float64 `json:"reward,omitempty"`
	Error               string  `json:"error,omitempty"`
}

func parseRunState(row *store.EvalRunRow) (*runState, error) {
	state := &runState{Status: string(row.Status)}
	if len(row.Results) == 0 {
		return state, nil
	}
	var partial map[string]any
	if err := json.Unmarshal(row.Results, &partial); err != nil {
		return nil, err
	}
	state.TaskCount = asInt(partial["task_count"])
	state.CompletedCount = asInt(partial["completed_count"])
	state.FailedCount = asInt(partial["failed_count"])
	rawTasks, _ := partial["tasks"].([]any)
	for _, raw := range rawTasks {
		taskMap, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		task := taskState{
			ID:             strVal(taskMap["id"]),
			Status:         strVal(taskMap["status"]),
			TrialTotal:     asInt(taskMap["trial_total"]),
			TrialPassCount: asInt(taskMap["trial_pass_count"]),
			Error:          strVal(taskMap["error"]),
		}
		if specRaw, ok := taskMap["spec"].(map[string]any); ok {
			task.Spec.ID = strVal(specRaw["id"])
			if msgs, ok := specRaw["messages"].([]any); ok {
				for _, m := range msgs {
					if s, ok := m.(string); ok {
						task.Spec.Messages = append(task.Spec.Messages, s)
					}
				}
			}
			task.Spec.TimeoutMs = asInt(specRaw["timeout_ms"])
			if rubricRaw, ok := specRaw["rubric"].(map[string]any); ok {
				task.Spec.Rubric = &rubricSpec{
					Description: strVal(rubricRaw["description"]),
				}
				if crit, ok := rubricRaw["criteria"].([]any); ok {
					for _, c := range crit {
						if s, ok := c.(string); ok && s != "" {
							task.Spec.Rubric.Criteria = append(
								task.Spec.Rubric.Criteria, s,
							)
						}
					}
				}
			}
		}
		if trialsRaw, ok := taskMap["trials"].([]any); ok {
			for _, tr := range trialsRaw {
				trMap, ok := tr.(map[string]any)
				if !ok {
					continue
				}
				trial := trialState{
					TrialIndex:          asInt(trMap["trial_index"]),
					Status:              strVal(trMap["status"]),
					SessionID:           strVal(trMap["session_id"]),
					CurrentMessageIndex: asInt(trMap["current_message_index"]),
					StartedAt:           strVal(trMap["started_at"]),
					EndedAt:             strVal(trMap["ended_at"]),
					TrajectoryID:        strVal(trMap["trajectory_id"]),
					Reward:              asFloat(trMap["reward"]),
					Error:               strVal(trMap["error"]),
				}
				task.Trials = append(task.Trials, trial)
			}
		}
		state.Tasks = append(state.Tasks, task)
	}
	if state.TaskCount == 0 {
		state.TaskCount = len(state.Tasks)
	}
	return state, nil
}

func marshalRunState(state *runState) (json.RawMessage, error) {
	tasks := make([]map[string]any, 0, len(state.Tasks))
	for _, task := range state.Tasks {
		trials := make([]map[string]any, 0, len(task.Trials))
		for _, trial := range task.Trials {
			entry := map[string]any{
				"trial_index": trial.TrialIndex,
				"status":      trial.Status,
			}
			if trial.SessionID != "" {
				entry["session_id"] = trial.SessionID
			}
			if trial.CurrentMessageIndex > 0 {
				entry["current_message_index"] = trial.CurrentMessageIndex
			}
			if trial.StartedAt != "" {
				entry["started_at"] = trial.StartedAt
			}
			if trial.EndedAt != "" {
				entry["ended_at"] = trial.EndedAt
			}
			if trial.TrajectoryID != "" {
				entry["trajectory_id"] = trial.TrajectoryID
			}
			if trial.Reward > 0 || trial.Status == "failed" {
				entry["reward"] = trial.Reward
			}
			if trial.Error != "" {
				entry["error"] = trial.Error
			}
			trials = append(trials, entry)
		}
		item := map[string]any{
			"id":     task.ID,
			"spec":   task.Spec,
			"status": task.Status,
			"trials": trials,
		}
		if task.TrialTotal > 0 {
			item["trial_total"] = task.TrialTotal
		}
		if task.TrialPassCount > 0 {
			item["trial_pass_count"] = task.TrialPassCount
		}
		if task.Error != "" {
			item["error"] = task.Error
		}
		tasks = append(tasks, item)
	}
	payload := map[string]any{
		"task_count":      state.TaskCount,
		"completed_count": state.CompletedCount,
		"failed_count":    state.FailedCount,
		"tasks":           tasks,
	}
	raw, err := json.Marshal(payload)
	return raw, err
}

func nowISO() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func strVal(v any) string {
	s, _ := v.(string)
	return s
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

func asFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	default:
		return 0
	}
}
