package session

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/store"
)

const outcomeIDPrefix = "outc_"
const outcomeIDAlphabet = "0123456789abcdefghijklmnopqrstuvwxyz"

// ActiveOutcome is the in-session outcome grader target (AMA-aligned).
type ActiveOutcome struct {
	OutcomeID     string          `json:"outcome_id"`
	Description   string          `json:"description"`
	Criteria      []string        `json:"criteria,omitempty"`
	Rubric        json.RawMessage `json:"rubric,omitempty"`
	RubricContent string          `json:"rubric_content,omitempty"`
	MaxIterations int             `json:"max_iterations,omitempty"`
}

// OutcomeEvaluationRecord is one terminal or in-progress evaluation row.
type OutcomeEvaluationRecord struct {
	OutcomeID   string `json:"outcome_id"`
	Result      string `json:"result"`
	Iteration   int    `json:"iteration"`
	Explanation string `json:"explanation,omitempty"`
	Feedback    string `json:"feedback,omitempty"`
}

type outcomeMetadata struct {
	Outcome            *ActiveOutcome            `json:"outcome"`
	OutcomeIteration   int                       `json:"outcome_iteration"`
	OutcomeEvaluations []OutcomeEvaluationRecord `json:"outcome_evaluations"`
}

func loadOutcomeMetadata(raw json.RawMessage) outcomeMetadata {
	var meta outcomeMetadata
	if len(raw) == 0 || string(raw) == "null" {
		return meta
	}
	_ = json.Unmarshal(raw, &meta)
	return meta
}

func (m *Machine) readOutcomeState(ctx context.Context) (outcomeMetadata, error) {
	if m.Sessions == nil {
		return outcomeMetadata{}, nil
	}
	sess, err := m.Sessions.Get(ctx, m.TenantID, m.SessionID)
	if err != nil || sess == nil {
		return outcomeMetadata{}, err
	}
	return loadOutcomeMetadata(sess.Metadata), nil
}

func (m *Machine) persistOutcomePatch(
	ctx context.Context,
	patch map[string]any,
) error {
	if m.Sessions == nil {
		return nil
	}
	raw, err := json.Marshal(patch)
	if err != nil {
		return err
	}
	_, err = m.Sessions.Update(ctx, m.TenantID, m.SessionID, store.UpdateSessionInput{
		Metadata:    raw,
		MetadataSet: true,
	})
	return err
}

// PrepareDefineOutcome validates and echoes a user.define_outcome event.
func PrepareDefineOutcome(raw json.RawMessage) (json.RawMessage, error) {
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, err
	}
	normalized, err := normalizeDefineOutcome(body)
	if err != nil {
		return nil, err
	}
	outcomeID, _ := normalized["outcome_id"].(string)
	if !strings.HasPrefix(outcomeID, outcomeIDPrefix) {
		normalized["outcome_id"] = mintOutcomeID()
	}
	normalized["type"] = "user.define_outcome"
	out, err := json.Marshal(normalized)
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ActivateOutcomeFromEvent stores active outcome state from define_outcome.
func (m *Machine) ActivateOutcomeFromEvent(
	ctx context.Context,
	raw json.RawMessage,
) error {
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return err
	}
	active, err := activeOutcomeFromDefineEvent(body)
	if err != nil {
		return err
	}
	return m.persistOutcomePatch(ctx, map[string]any{
		"outcome":            active,
		"outcome_iteration":  0,
	})
}

func normalizeDefineOutcome(body map[string]any) (map[string]any, error) {
	out := map[string]any{}
	for key, val := range body {
		out[key] = val
	}
	desc, _ := out["description"].(string)
	var criteria []string
	if nested, ok := out["outcome"].(map[string]any); ok {
		if desc == "" {
			desc, _ = nested["description"].(string)
		}
		criteria = stringSliceFromAny(nested["criteria"])
		if out["rubric"] == nil {
			out["rubric"] = nested["rubric"]
		}
		if out["max_iterations"] == nil {
			out["max_iterations"] = nested["max_iterations"]
		}
	}
	inlineRubric := rubricTextFromAny(out["rubric"])
	if inlineRubric == "" && len(criteria) > 0 && !hasRubricSpec(out["rubric"]) {
		inlineRubric = strings.Join(criteria, "\n")
		out["rubric"] = inlineRubric
	}
	if desc == "" {
		desc = inlineRubric
	}
	if strings.TrimSpace(desc) == "" &&
		!hasRubricSpec(out["rubric"]) &&
		len(criteria) == 0 {
		return nil, fmt.Errorf(
			"user.define_outcome requires description or rubric",
		)
	}
	out["description"] = desc
	if _, ok := out["outcome"]; !ok {
		legacy := map[string]any{"description": desc}
		if len(criteria) > 0 {
			legacy["criteria"] = criteria
		}
		if raw := out["rubric"]; raw != nil {
			legacy["rubric"] = raw
		}
		out["outcome"] = legacy
	}
	return out, nil
}

func activeOutcomeFromDefineEvent(body map[string]any) (*ActiveOutcome, error) {
	normalized, err := normalizeDefineOutcome(body)
	if err != nil {
		return nil, err
	}
	outcomeID, _ := normalized["outcome_id"].(string)
	desc, _ := normalized["description"].(string)
	criteria := stringSliceFromAny(
		mapLookup(normalized, "outcome", "criteria"),
	)
	maxIter := clampMaxIterations(normalized["max_iterations"])
	return &ActiveOutcome{
		OutcomeID:     outcomeID,
		Description:   desc,
		Criteria:      criteria,
		Rubric:        rubricRawFromAny(normalized["rubric"]),
		MaxIterations: maxIter,
	}, nil
}

func mapLookup(root map[string]any, keys ...string) any {
	cur := any(root)
	for _, key := range keys {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[key]
	}
	return cur
}

func rubricTextFromAny(raw any) string {
	switch v := raw.(type) {
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		if v["type"] == "text" {
			if text, ok := v["content"].(string); ok {
				return strings.TrimSpace(text)
			}
		}
	}
	return ""
}

func stringSliceFromAny(raw any) []string {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if ok && strings.TrimSpace(s) != "" {
			out = append(out, strings.TrimSpace(s))
		}
	}
	return out
}

func clampMaxIterations(raw any) int {
	n, ok := raw.(float64)
	if !ok || n < 1 {
		return 3
	}
	if n > 20 {
		return 20
	}
	return int(n)
}

func mintOutcomeID() string {
	out := make([]byte, 16)
	max := big.NewInt(int64(len(outcomeIDAlphabet)))
	for i := range out {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(err)
		}
		out[i] = outcomeIDAlphabet[idx.Int64()]
	}
	return outcomeIDPrefix + string(out)
}

func (m *Machine) ensureOutcomeRubricResolved(
	ctx context.Context,
	outcome ActiveOutcome,
) (ActiveOutcome, error) {
	if strings.TrimSpace(outcome.RubricContent) != "" {
		return outcome, nil
	}
	if len(outcome.Rubric) == 0 {
		return outcome, nil
	}
	content, err := ResolveRubric(
		ctx, m.TenantID, outcome.Rubric, m.Resources,
	)
	if err != nil {
		return outcome, err
	}
	outcome.RubricContent = content
	if err := m.persistOutcomePatch(ctx, map[string]any{
		"outcome": outcome,
	}); err != nil {
		return outcome, err
	}
	return outcome, nil
}

func (o *ActiveOutcome) harnessRubric() harness.OutcomeRubric {
	rubricText := strings.TrimSpace(o.RubricContent)
	if rubricText == "" {
		rubricText = rubricTextFromRaw(o.Rubric)
	}
	desc := rubricText
	if desc == "" {
		desc = o.Description
	}
	criteria := o.Criteria
	if len(criteria) == 0 && rubricText != "" {
		for _, line := range strings.Split(rubricText, "\n") {
			line = strings.TrimSpace(line)
			if line != "" {
				criteria = append(criteria, line)
			}
		}
	}
	return harness.OutcomeRubric{
		Description: desc,
		Criteria:    criteria,
	}
}

func rubricTextFromRaw(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var inline string
	if err := json.Unmarshal(raw, &inline); err == nil {
		return strings.TrimSpace(inline)
	}
	var spec struct {
		Type    string `json:"type"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		return ""
	}
	if spec.Type == "text" {
		return strings.TrimSpace(spec.Content)
	}
	return ""
}
