package api

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/store"
)

func TestDeriveTrajectoryOutcome(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		events []store.StoredEvent
		status store.SessionStatus
		want   string
	}{
		{
			name: "session error is failure",
			events: []store.StoredEvent{
				{Type: "session.error"},
			},
			status: store.SessionStatusIdle,
			want:   "failure",
		},
		{
			name: "interrupt after idle wins",
			events: []store.StoredEvent{
				{Type: "session.status_idle"},
				{Type: "user.interrupt"},
			},
			status: store.SessionStatusIdle,
			want:   "interrupted",
		},
		{
			name:   "idle session without events",
			events: nil,
			status: store.SessionStatusIdle,
			want:   "success",
		},
		{
			name:   "running session without terminal events",
			events: nil,
			status: store.SessionStatusRunning,
			want:   "running",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := deriveTrajectoryOutcome(tc.events, tc.status)
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestComputeTrajectorySummary(t *testing.T) {
	t.Parallel()
	start := time.Now().UTC().Format(time.RFC3339Nano)
	end := time.Now().Add(2 * time.Second).UTC().Format(time.RFC3339Nano)

	events := []store.StoredEvent{
		{
			Type: "agent.tool_use",
			Payload: json.RawMessage(`{"name":"bash"}`),
		},
		{
			Type: "agent.tool_result",
			Payload: json.RawMessage(`{"is_error":true}`),
		},
		{
			Type: "agent.message",
			Payload: json.RawMessage(`{"content":[]}`),
		},
		{
			Type: "span.model_request_end",
			Payload: json.RawMessage(`{
				"model_usage": {
					"input_tokens": 10,
					"output_tokens": 5
				}
			}`),
		},
	}

	summary := computeTrajectorySummary(events, start, end)
	if summary.NumEvents != 4 {
		t.Fatalf("num_events=%d want 4", summary.NumEvents)
	}
	if summary.NumTurns != 1 {
		t.Fatalf("num_turns=%d want 1", summary.NumTurns)
	}
	if summary.NumToolCalls != 1 {
		t.Fatalf("num_tool_calls=%d want 1", summary.NumToolCalls)
	}
	if summary.NumToolErrors != 1 {
		t.Fatalf("num_tool_errors=%d want 1", summary.NumToolErrors)
	}
	if summary.TokenUsage.InputTokens != 10 {
		t.Fatalf("input_tokens=%d want 10", summary.TokenUsage.InputTokens)
	}
	if summary.DurationMs <= 0 {
		t.Fatalf("duration_ms=%d want > 0", summary.DurationMs)
	}
}

func TestBuildTrajectoryIncludesSnapshots(t *testing.T) {
	t.Parallel()
	now := time.Now().UnixMilli()
	sess := &store.Session{
		ID:                  "sess_test",
		CreatedAt:           now,
		Status:              store.SessionStatusIdle,
		AgentSnapshot:       json.RawMessage(`{"id":"agt_x","model":"claude-sonnet"}`),
		EnvironmentSnapshot: json.RawMessage(`{"id":"env_x","name":"default"}`),
	}
	events := []store.StoredEvent{
		{
			Seq:       1,
			Type:      "agent.message",
			Payload:   json.RawMessage(`{"type":"agent.message","content":[]}`),
			CreatedAt: now,
		},
	}

	traj := buildTrajectory(sess, events)
	if traj["schema_version"] != "oma.trajectory.v1" {
		t.Fatalf("schema_version=%v", traj["schema_version"])
	}
	agentCfg, ok := traj["agent_config"].(map[string]any)
	if !ok || agentCfg["id"] != "agt_x" {
		t.Fatalf("agent_config=%v", traj["agent_config"])
	}
	envCfg, ok := traj["environment_config"].(map[string]any)
	if !ok || envCfg["id"] != "env_x" {
		t.Fatalf("environment_config=%v", traj["environment_config"])
	}
	model, ok := traj["model"].(map[string]any)
	if !ok || model["id"] != "claude-sonnet" {
		t.Fatalf("model=%v", traj["model"])
	}
	evs, ok := traj["events"].([]trajectoryEvent)
	if !ok || len(evs) != 1 {
		t.Fatalf("events=%T len=%d", traj["events"], len(evs))
	}
	summary, ok := traj["summary"].(trajectorySummary)
	if !ok || summary.NumTurns != 1 {
		t.Fatalf("summary=%v", traj["summary"])
	}
}
