package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/store"
)

func TestCostReportAggregatesUsageSpans(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	handler, _, sessions := testRouterSharedDB(t, db, &harness.FakeClient{})
	ctx := context.Background()

	agentBody := `{"name":"cost-agent","model":"claude-sonnet-4-20250514"}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/agents", bytes.NewBufferString(agentBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create agent status=%d", rec.Code)
	}
	var agent map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &agent)
	agentID := agent["id"].(string)

	sess, err := sessions.Create(ctx, store.CreateSessionInput{
		AgentID: agentID,
	})
	if err != nil {
		t.Fatal(err)
	}

	events := store.NewEventRepo(db)
	payload, _ := json.Marshal(map[string]any{
		"type": "span.model_request_end",
		"model_usage": map[string]any{
			"input_tokens":  12,
			"output_tokens": 4,
		},
	})
	if _, err := events.AppendEvents(ctx, sess.ID, []json.RawMessage{payload}); err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/cost_report?days=30", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cost_report status=%d body=%s", rec.Code, rec.Body.String())
	}
	var report map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &report)
	if report["type"] != "cost_report" {
		t.Fatalf("type=%v", report["type"])
	}
	usage, ok := report["usage"].(map[string]any)
	if !ok {
		t.Fatalf("usage=%v", report["usage"])
	}
	if int(usage["input_tokens"].(float64)) != 12 {
		t.Fatalf("input_tokens=%v", usage["input_tokens"])
	}
	if int(report["span_count"].(float64)) != 1 {
		t.Fatalf("span_count=%v", report["span_count"])
	}
}
