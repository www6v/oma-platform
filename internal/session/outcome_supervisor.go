package session

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/open-ma/oma-building/internal/eval"
	"github.com/open-ma/oma-building/internal/harness"
)

var terminalOutcomeResults = map[string]struct{}{
	"satisfied":              {},
	"max_iterations_reached": {},
	"failed":                 {},
	"interrupted":            {},
}

// OutcomeSupervisorDeps configures the in-session grader loop.
type OutcomeSupervisorDeps struct {
	Machine          *Machine
	Evaluator        harness.OutcomeEvaluator
	InitialIteration int
	RunHarnessTurn   func(ctx context.Context) error
}

// RunOutcomeSupervisor evaluates agent output and may schedule revision turns.
func RunOutcomeSupervisor(
	ctx context.Context,
	deps OutcomeSupervisorDeps,
) error {
	if deps.Machine == nil || deps.Evaluator == nil {
		return nil
	}
	state, err := deps.Machine.readOutcomeState(ctx)
	if err != nil || state.Outcome == nil {
		return err
	}
	outcome := *state.Outcome
	maxIterations := outcome.MaxIterations
	if maxIterations < 1 {
		maxIterations = 3
	}
	iteration := deps.InitialIteration
	if iteration < 0 {
		iteration = state.OutcomeIteration
	}

	resolved, resolveErr := deps.Machine.ensureOutcomeRubricResolved(ctx, outcome)
	if resolveErr != nil {
		return deps.Machine.emitOutcomeVerdict(ctx, outcome, iteration, outcomeVerdict{
			result:      "failed",
			explanation: resolveErr.Error(),
			withStart:   false,
		}, state.OutcomeEvaluations)
	}
	outcome = resolved

	modelCfg, err := deps.Machine.resolveOutcomeJudgeModel(ctx)
	if err != nil {
		return deps.Machine.emitOutcomeVerdict(ctx, outcome, iteration, outcomeVerdict{
			result:      "failed",
			explanation: err.Error(),
			withStart:   false,
		}, state.OutcomeEvaluations)
	}

	for iteration < maxIterations {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		startID := mintOutcomeID()
		lastAgentID := findLastAgentMessageID(ctx, deps.Machine)
		start := map[string]any{
			"type":       "span.outcome_evaluation_start",
			"id":         startID,
			"outcome_id": outcome.OutcomeID,
			"iteration":  iteration,
		}
		if lastAgentID != "" {
			start["parent_event_id"] = lastAgentID
		}
		if err := deps.Machine.publishEvents(ctx, []json.RawMessage{
			mustJSON(start),
			mustJSON(map[string]any{
				"type":       "span.outcome_evaluation_ongoing",
				"outcome_id": outcome.OutcomeID,
				"iteration":  iteration,
			}),
		}); err != nil {
			return err
		}

		output, err := deps.Machine.agentOutputFromHistory(ctx)
		if err != nil {
			return err
		}
		evalResp, evalErr := deps.Evaluator.EvaluateOutcome(ctx, harness.OutcomeEvaluateRequest{
			Rubric:      outcome.harnessRubric(),
			AgentOutput: output,
			Model:       modelCfg,
		})

		var verdict outcomeVerdict
		if evalErr != nil {
			verdict = outcomeVerdict{
				result:      "failed",
				explanation: evalErr.Error(),
				withStart:   true,
				startID:     startID,
				parentID:    lastAgentID,
			}
		} else {
			verdict = mapEvaluatorVerdict(
				evalResp, iteration, maxIterations,
			)
			verdict.withStart = true
			verdict.startID = startID
			verdict.parentID = lastAgentID
		}

		evals := state.OutcomeEvaluations
		if err := deps.Machine.emitOutcomeVerdict(
			ctx, outcome, iteration, verdict, evals,
		); err != nil {
			return err
		}
		state, err = deps.Machine.readOutcomeState(ctx)
		if err != nil {
			return err
		}
		evals = state.OutcomeEvaluations

		if isTerminalOutcomeResult(verdict.result) {
			return nil
		}

		iteration++
		if err := deps.Machine.persistOutcomePatch(ctx, map[string]any{
			"outcome":           outcome,
			"outcome_iteration": iteration,
		}); err != nil {
			return err
		}

		feedback := verdict.explanation
		if feedback == "" {
			feedback = "(no explanation)"
		}
		feedbackEvent, err := json.Marshal(map[string]any{
			"type": "user.message",
			"content": []map[string]string{
				{
					"type": "text",
					"text": fmt.Sprintf(
						"<outcome_feedback iteration=\"%d\">\n%s\n\n"+
							"Address the feedback and try again.\n"+
							"</outcome_feedback>",
						iteration-1,
						feedback,
					),
				},
			},
		})
		if err != nil {
			return err
		}
		if err := deps.Machine.publishEvents(ctx, []json.RawMessage{feedbackEvent}); err != nil {
			return err
		}
		if deps.RunHarnessTurn == nil {
			return fmt.Errorf("outcome supervisor missing RunHarnessTurn")
		}
		if err := deps.RunHarnessTurn(ctx); err != nil {
			return deps.Machine.emitOutcomeVerdict(ctx, outcome, iteration, outcomeVerdict{
				result:      "failed",
				explanation: fmt.Sprintf(
					"harness crashed during revision: %v", err,
				),
				withStart: false,
			}, evals)
		}
	}

	return deps.Machine.emitOutcomeVerdict(ctx, outcome, iteration-1, outcomeVerdict{
		result:      "max_iterations_reached",
		explanation: "supervisor loop exited without a terminal verdict",
		withStart:   false,
	}, state.OutcomeEvaluations)
}

type outcomeVerdict struct {
	result      string
	explanation string
	withStart   bool
	startID     string
	parentID    string
}

func mapEvaluatorVerdict(
	resp harness.OutcomeEvaluateResponse,
	iteration, maxIterations int,
) outcomeVerdict {
	feedback := resp.Feedback
	if feedback == "" {
		feedback = resp.Result
	}
	result := strings.ToLower(strings.TrimSpace(resp.Result))
	switch result {
	case "satisfied", "pass", "passed":
		return outcomeVerdict{result: "satisfied", explanation: feedback}
	case "needs_revision", "fail", "failed", "reject", "rejected":
		if iteration >= maxIterations-1 {
			return outcomeVerdict{
				result:      "max_iterations_reached",
				explanation: feedback,
			}
		}
		return outcomeVerdict{
			result:      "needs_revision",
			explanation: feedback,
		}
	default:
		if iteration >= maxIterations-1 {
			return outcomeVerdict{
				result:      "max_iterations_reached",
				explanation: feedback,
			}
		}
		return outcomeVerdict{
			result:      "needs_revision",
			explanation: feedback,
		}
	}
}

func (m *Machine) emitOutcomeVerdict(
	ctx context.Context,
	outcome ActiveOutcome,
	iteration int,
	verdict outcomeVerdict,
	prior []OutcomeEvaluationRecord,
) error {
	end := map[string]any{
		"type":        "span.outcome_evaluation_end",
		"outcome_id":  outcome.OutcomeID,
		"result":      verdict.result,
		"iteration":   iteration,
		"explanation": verdict.explanation,
		"feedback":    verdict.explanation,
	}
	if verdict.withStart && verdict.startID != "" {
		end["outcome_evaluation_start_id"] = verdict.startID
	}
	if verdict.parentID != "" {
		end["parent_event_id"] = verdict.parentID
	}
	if err := m.publishEvents(ctx, []json.RawMessage{mustJSON(end)}); err != nil {
		return err
	}

	record := OutcomeEvaluationRecord{
		OutcomeID:   outcome.OutcomeID,
		Result:      verdict.result,
		Iteration:   iteration,
		Explanation: verdict.explanation,
		Feedback:    verdict.explanation,
	}
	persisted := append(append([]OutcomeEvaluationRecord{}, prior...), record)
	patch := map[string]any{"outcome_evaluations": persisted}
	if isTerminalOutcomeResult(verdict.result) {
		patch["outcome"] = nil
	} else {
		patch["outcome"] = outcome
		patch["outcome_iteration"] = iteration
	}
	return m.persistOutcomePatch(ctx, patch)
}

func (m *Machine) resolveOutcomeJudgeModel(ctx context.Context) (harness.ModelConfig, error) {
	sess, err := m.Sessions.Get(ctx, m.TenantID, m.SessionID)
	if err != nil || sess == nil {
		return harness.ModelConfig{}, fmt.Errorf("session not found")
	}
	agent, err := harness.AgentSnapshotFromRaw(sess.AgentSnapshot)
	if err != nil {
		return harness.ModelConfig{}, err
	}
	modelID := agent.AuxModel
	if modelID == "" {
		modelID = agent.Model
	}
	return m.resolveModel(ctx, modelID)
}

func (m *Machine) agentOutputFromHistory(ctx context.Context) (string, error) {
	if m.Events == nil {
		return "", nil
	}
	history, err := m.Events.ListEvents(ctx, m.SessionID, 0, 10000, true)
	if err != nil {
		return "", err
	}
	payloads := make([]json.RawMessage, 0, len(history))
	for _, ev := range history {
		payloads = append(payloads, ev.Payload)
	}
	return eval.LastAgentOutputFromEvents(payloads), nil
}

func (m *Machine) maybeRunOutcomeSupervisor(
	ctx context.Context,
	threadID string,
) error {
	if threadID != "" && threadID != defaultThreadID {
		return nil
	}
	state, err := m.readOutcomeState(ctx)
	if err != nil || state.Outcome == nil {
		return err
	}
	payloads, err := m.listEventPayloads(ctx)
	if err != nil {
		return err
	}
	if len(harness.PendingCustomToolIDs(payloads)) > 0 {
		return nil
	}
	evaluator := m.OutcomeEvaluator
	if evaluator == nil {
		evaluator = harness.AsOutcomeEvaluator(m.Harness)
	}
	if evaluator == nil {
		return m.emitOutcomeVerdict(ctx, *state.Outcome, state.OutcomeIteration, outcomeVerdict{
			result:      "failed",
			explanation: "outcome evaluator unavailable",
			withStart:   false,
		}, state.OutcomeEvaluations)
	}
	return RunOutcomeSupervisor(ctx, OutcomeSupervisorDeps{
		Machine:          m,
		Evaluator:        evaluator,
		InitialIteration: state.OutcomeIteration,
		RunHarnessTurn: func(runCtx context.Context) error {
			return m.runSingleHarnessTurn(runCtx, defaultThreadID)
		},
	})
}

func (m *Machine) listEventPayloads(ctx context.Context) ([]json.RawMessage, error) {
	if m.Events == nil {
		return nil, nil
	}
	history, err := m.Events.ListEvents(ctx, m.SessionID, 0, 10000, true)
	if err != nil {
		return nil, err
	}
	out := make([]json.RawMessage, 0, len(history))
	for _, ev := range history {
		out = append(out, ev.Payload)
	}
	return out, nil
}

func findLastAgentMessageID(
	ctx context.Context,
	m *Machine,
) string {
	payloads, err := m.listEventPayloads(ctx)
	if err != nil {
		return ""
	}
	for i := len(payloads) - 1; i >= 0; i-- {
		var ev map[string]any
		if json.Unmarshal(payloads[i], &ev) != nil {
			continue
		}
		if ev["type"] != "agent.message" {
			continue
		}
		if id, ok := ev["id"].(string); ok {
			return id
		}
	}
	return ""
}

func isTerminalOutcomeResult(result string) bool {
	_, ok := terminalOutcomeResults[result]
	return ok
}

func mustJSON(v map[string]any) json.RawMessage {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return raw
}
