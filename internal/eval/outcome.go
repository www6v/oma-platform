package eval

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/store"
)

type rubricSpec struct {
	Description string   `json:"description"`
	Criteria    []string `json:"criteria,omitempty"`
}

func (w *Worker) scoreTrial(
	ctx context.Context,
	row *store.EvalRunRow,
	task *taskState,
	trial *trialState,
) (float64, string, error) {
	if task.Spec.Rubric == nil || task.Spec.Rubric.Description == "" {
		return 1, "", nil
	}
	if w.Evaluator == nil || w.Events == nil {
		return 1, "", nil
	}

	events, err := w.Events.ListEvents(ctx, trial.SessionID, 0, 10000, true)
	if err != nil {
		return 0, "", err
	}
	payloads := make([]json.RawMessage, 0, len(events))
	for i := range events {
		payloads = append(payloads, events[i].Payload)
	}
	output := AgentOutputFromEvents(payloads)
	if output == "" {
		return 0, "no agent output to evaluate", nil
	}

	modelCfg, err := w.resolveJudgeModel(ctx, row.TenantID, row.AgentID)
	if err != nil {
		return 0, "", err
	}

	result, err := w.Evaluator.EvaluateOutcome(ctx, harness.OutcomeEvaluateRequest{
		Rubric: harness.OutcomeRubric{
			Description: task.Spec.Rubric.Description,
			Criteria:    task.Spec.Rubric.Criteria,
		},
		AgentOutput: output,
		Model:       modelCfg,
	})
	if err != nil {
		return 0, "", err
	}
	if result.Result == "satisfied" {
		return 1, result.Feedback, nil
	}
	msg := result.Feedback
	if msg == "" {
		msg = "rubric not satisfied"
	}
	return 0, msg, nil
}

func (w *Worker) resolveJudgeModel(
	ctx context.Context,
	tenantID, agentID string,
) (harness.ModelConfig, error) {
	if w.Agents == nil || w.Models == nil {
		return harness.ModelConfig{}, fmt.Errorf("judge model deps unavailable")
	}
	agent, err := w.Agents.Get(ctx, tenantID, agentID)
	if err != nil || agent == nil {
		return harness.ModelConfig{}, fmt.Errorf("agent %s not found", agentID)
	}
	modelID := agent.AuxModel
	if modelID == "" {
		modelID = agent.Model
	}
	resolved, err := w.Models.Resolve(ctx, tenantID, modelID)
	if err != nil {
		return harness.ModelConfig{}, err
	}
	return harness.ModelConfig{
		Model:         resolved.Model,
		Provider:      resolved.Provider,
		APIKey:        resolved.APIKey,
		BaseURL:       resolved.BaseURL,
		CustomHeaders: resolved.CustomHeaders,
	}, nil
}
