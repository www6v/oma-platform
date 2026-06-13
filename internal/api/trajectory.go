package api

import (
	"encoding/json"
	"time"

	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/usage"
)

type trajectoryEvent struct {
	Seq  int             `json:"seq"`
	Type string          `json:"type"`
	Ts   string          `json:"ts"`
	Data json.RawMessage `json:"data"`
}

type trajectorySummary struct {
	NumEvents     int `json:"num_events"`
	NumTurns      int `json:"num_turns"`
	NumToolCalls  int `json:"num_tool_calls"`
	NumToolErrors int `json:"num_tool_errors"`
	NumThreads    int `json:"num_threads"`
	DurationMs    int `json:"duration_ms"`
	TokenUsage    usage.TokenTotals `json:"token_usage"`
}

func buildTrajectory(sess *store.Session, events []store.StoredEvent) map[string]any {
	trajEvents := make([]trajectoryEvent, 0, len(events))
	for _, ev := range events {
		trajEvents = append(trajEvents, trajectoryEvent{
			Seq:  ev.Seq,
			Type: ev.Type,
			Ts:   formatISO(ev.CreatedAt),
			Data: ev.Payload,
		})
	}

	startedAt := time.UnixMilli(sess.CreatedAt).UTC().Format(time.RFC3339Nano)
	outcome := deriveTrajectoryOutcome(events, sess.Status)
	endedAt := deriveTrajectoryEndedAt(events, outcome, sess)

	summary := computeTrajectorySummary(events, startedAt, endedAt)

	numThreads := summary.NumThreads
	if numThreads == 0 {
		numThreads = 1
	}
	summary.NumThreads = numThreads

	return map[string]any{
		"schema_version":     "oma.trajectory.v1",
		"trajectory_id":      "traj_" + sess.ID,
		"session_id":         sess.ID,
		"agent_config":       jsonAny(sess.AgentSnapshot),
		"environment_config": jsonAny(sess.EnvironmentSnapshot),
		"model": map[string]any{
			"id":       modelIDFromSnapshot(sess.AgentSnapshot),
			"provider": "oma-platform",
		},
		"started_at": startedAt,
		"ended_at":   nullIfEmpty(endedAt),
		"outcome":    outcome,
		"events":     trajEvents,
		"summary":    summary,
	}
}

func deriveTrajectoryOutcome(
	events []store.StoredEvent,
	status store.SessionStatus,
) string {
	for i := len(events) - 1; i >= 0; i-- {
		switch events[i].Type {
		case "session.error":
			return "failure"
		case "user.interrupt":
			return "interrupted"
		case "session.status_terminated":
			return "interrupted"
		case "session.status_idle":
			return "success"
		case "session.status_running":
			return "running"
		}
	}
	switch status {
	case store.SessionStatusRunning:
		return "running"
	case store.SessionStatusInterrupted:
		return "interrupted"
	default:
		return "success"
	}
}

func deriveTrajectoryEndedAt(
	events []store.StoredEvent,
	outcome string,
	sess *store.Session,
) string {
	if outcome == "running" {
		return ""
	}
	if len(events) > 0 {
		last := events[len(events)-1]
		return time.UnixMilli(last.CreatedAt).UTC().Format(time.RFC3339Nano)
	}
	if sess.UpdatedAt != nil {
		return time.UnixMilli(*sess.UpdatedAt).UTC().Format(time.RFC3339Nano)
	}
	return time.UnixMilli(sess.CreatedAt).UTC().Format(time.RFC3339Nano)
}

func computeTrajectorySummary(
	events []store.StoredEvent,
	startedAt, endedAt string,
) trajectorySummary {
	var summary trajectorySummary
	summary.NumEvents = len(events)

	threadIDs := make(map[string]struct{})
	for _, ev := range events {
		data := parseTrajectoryEventData(ev.Payload)
		switch ev.Type {
		case "agent.message":
			summary.NumTurns++
		case "agent.tool_use", "agent.custom_tool_use", "agent.mcp_tool_use":
			summary.NumToolCalls++
		case "agent.tool_result", "agent.mcp_tool_result":
			if isTrajectoryToolError(data) {
				summary.NumToolErrors++
			}
		case "session.thread_created":
			if tid, ok := data["session_thread_id"].(string); ok && tid != "" {
				threadIDs[tid] = struct{}{}
			}
		case "span.model_request_end":
			usage.ApplySpanUsage(data, &summary.TokenUsage)
		}
	}
	summary.NumThreads = len(threadIDs)
	if endedAt != "" {
		start, errStart := time.Parse(time.RFC3339Nano, startedAt)
		end, errEnd := time.Parse(time.RFC3339Nano, endedAt)
		if errStart == nil && errEnd == nil {
			ms := end.Sub(start).Milliseconds()
			if ms > 0 {
				summary.DurationMs = int(ms)
			}
		}
	}
	return summary
}

func parseTrajectoryEventData(payload json.RawMessage) map[string]any {
	if len(payload) == 0 {
		return nil
	}
	var data map[string]any
	if err := json.Unmarshal(payload, &data); err != nil {
		return nil
	}
	return data
}

func isTrajectoryToolError(data map[string]any) bool {
	if data == nil {
		return false
	}
	isErr, ok := data["is_error"].(bool)
	return ok && isErr
}

func modelIDFromSnapshot(raw json.RawMessage) string {
	if len(raw) == 0 {
		return "unknown"
	}
	var cfg struct {
		Model json.RawMessage `json:"model"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil || len(cfg.Model) == 0 {
		return "unknown"
	}
	var modelStr string
	if err := json.Unmarshal(cfg.Model, &modelStr); err == nil && modelStr != "" {
		return modelStr
	}
	var modelObj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(cfg.Model, &modelObj); err == nil && modelObj.ID != "" {
		return modelObj.ID
	}
	return "unknown"
}

func jsonAny(raw json.RawMessage) any {
	if len(raw) == 0 {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return map[string]any{}
	}
	return v
}
