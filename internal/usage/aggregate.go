package usage

import (
	"encoding/json"

	"github.com/open-ma/oma-building/internal/store"
)

// TokenTotals aggregates model token usage.
type TokenTotals struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

// AgentUsage is per-agent token totals.
type AgentUsage struct {
	AgentID string `json:"agent_id"`
	TokenTotals
	SpanCount int `json:"span_count"`
}

// Report is a tenant usage summary.
type Report struct {
	PeriodDays   int          `json:"period_days"`
	SinceMs      int64        `json:"since_ms"`
	UntilMs      int64        `json:"until_ms"`
	Usage        TokenTotals  `json:"usage"`
	ByAgent      []AgentUsage `json:"by_agent"`
	SessionCount int          `json:"session_count"`
	SpanCount    int          `json:"span_count"`
}

// ApplySpanUsage adds model_usage fields from a span payload.
func ApplySpanUsage(data map[string]any, totals *TokenTotals) {
	if data == nil || totals == nil {
		return
	}
	usageRaw, ok := data["model_usage"].(map[string]any)
	if !ok {
		return
	}
	totals.InputTokens += intNumber(usageRaw["input_tokens"])
	totals.OutputTokens += intNumber(usageRaw["output_tokens"])
	totals.CacheReadInputTokens += intNumber(
		usageRaw["cache_read_input_tokens"],
	)
	totals.CacheCreationInputTokens += intNumber(
		usageRaw["cache_creation_input_tokens"],
	)
}

// AggregateEvents sums usage spans from stored session events.
func AggregateEvents(events []store.StoredEvent) TokenTotals {
	var totals TokenTotals
	for _, ev := range events {
		if ev.Type != "span.model_request_end" {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal(ev.Payload, &data); err != nil {
			continue
		}
		ApplySpanUsage(data, &totals)
	}
	return totals
}

// BuildReport aggregates usage rows into a tenant report.
func BuildReport(
	rows []store.UsageEventRow,
	periodDays int,
	sinceMs, untilMs int64,
) Report {
	byAgent := make(map[string]*AgentUsage)
	sessions := make(map[string]struct{})
	report := Report{
		PeriodDays: periodDays,
		SinceMs:    sinceMs,
		UntilMs:    untilMs,
	}
	for _, row := range rows {
		sessions[row.SessionID] = struct{}{}
		report.SpanCount++
		var data map[string]any
		if err := json.Unmarshal(row.Payload, &data); err != nil {
			continue
		}
		ApplySpanUsage(data, &report.Usage)
		entry, ok := byAgent[row.AgentID]
		if !ok {
			entry = &AgentUsage{AgentID: row.AgentID}
			byAgent[row.AgentID] = entry
		}
		entry.SpanCount++
		ApplySpanUsage(data, &entry.TokenTotals)
	}
	report.SessionCount = len(sessions)
	report.ByAgent = make([]AgentUsage, 0, len(byAgent))
	for _, entry := range byAgent {
		report.ByAgent = append(report.ByAgent, *entry)
	}
	return report
}

func intNumber(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case int64:
		return int(n)
	default:
		return 0
	}
}
