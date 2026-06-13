package usage_test

import (
	"encoding/json"
	"testing"

	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/usage"
)

func TestAggregateEvents(t *testing.T) {
	t.Parallel()
	events := []store.StoredEvent{
		{
			Type: "span.model_request_end",
			Payload: json.RawMessage(`{
				"model_usage": {
					"input_tokens": 10,
					"output_tokens": 5
				}
			}`),
		},
		{
			Type: "agent.message",
			Payload: json.RawMessage(`{}`),
		},
	}
	totals := usage.AggregateEvents(events)
	if totals.InputTokens != 10 || totals.OutputTokens != 5 {
		t.Fatalf("totals=%+v", totals)
	}
}

func TestBuildReportGroupsByAgent(t *testing.T) {
	t.Parallel()
	rows := []store.UsageEventRow{
		{
			SessionID: "sess_a",
			AgentID:   "agent_a",
			Payload: json.RawMessage(`{
				"model_usage": {"input_tokens": 3, "output_tokens": 1}
			}`),
		},
		{
			SessionID: "sess_b",
			AgentID:   "agent_b",
			Payload: json.RawMessage(`{
				"model_usage": {"input_tokens": 7, "output_tokens": 2}
			}`),
		},
	}
	report := usage.BuildReport(rows, 7, 1, 2)
	if report.Usage.InputTokens != 10 {
		t.Fatalf("usage=%+v", report.Usage)
	}
	if report.SessionCount != 2 || report.SpanCount != 2 {
		t.Fatalf("counts session=%d span=%d", report.SessionCount, report.SpanCount)
	}
	if len(report.ByAgent) != 2 {
		t.Fatalf("by_agent=%d", len(report.ByAgent))
	}
}
