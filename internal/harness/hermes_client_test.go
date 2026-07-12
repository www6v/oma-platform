package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// hermesRunStub is a minimal Runs-API test server. It accepts
// POST /v1/runs and returns a fixed run_id, then serves the supplied
// SSE lines at GET /v1/runs/{id}/events. Also records the create-run
// request body for assertions.
type hermesRunStub struct {
	mu       sync.Mutex
	lastBody []byte
	lastAuth string
	runID    string
	// events is the list of raw JSON objects served as SSE data lines.
	events []string
	// createStatus overrides the POST /v1/runs status code.
	createStatus int
	// createBody overrides the POST /v1/runs response body.
	createBody string
}

func newHermesRunStub(runID string, events []string) (*hermesRunStub, *httptest.Server) {
	stub := &hermesRunStub{runID: runID, events: events}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/runs":
			body, _ := io.ReadAll(r.Body)
			stub.mu.Lock()
			stub.lastAuth = r.Header.Get("Authorization")
			stub.lastBody = body
			if stub.createStatus != 0 {
				w.WriteHeader(stub.createStatus)
				if stub.createBody != "" {
					_, _ = w.Write([]byte(stub.createBody))
				} else {
					resp, _ := json.Marshal(map[string]string{"run_id": stub.runID})
					_, _ = w.Write(resp)
				}
			} else {
				resp, _ := json.Marshal(map[string]string{"run_id": stub.runID})
				_, _ = w.Write(resp)
			}
			stub.mu.Unlock()

		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/runs/"):
			if r.Header.Get("Accept") != "text/event-stream" {
				t := "expected Accept: text/event-stream"
				http.Error(w, t, http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			for _, ev := range stub.events {
				_, _ = fmt.Fprintf(w, "data: %s\n\n", ev)
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}

		default:
			http.NotFound(w, r)
		}
	}))
	return stub, ts
}

// sse helpers — build a JSON string for one SSE data line.
func sseToolStarted(tool, preview string) string {
	b, _ := json.Marshal(map[string]any{
		"event": "tool.started", "run_id": "run_1",
		"tool": tool, "preview": preview,
	})
	return string(b)
}

func sseToolCompleted(tool string, duration float64, errored bool) string {
	b, _ := json.Marshal(map[string]any{
		"event": "tool.completed", "run_id": "run_1",
		"tool": tool, "duration": duration, "error": errored,
	})
	return string(b)
}

func sseDelta(text string) string {
	b, _ := json.Marshal(map[string]any{
		"event": "message.delta", "run_id": "run_1", "delta": text,
	})
	return string(b)
}

func sseRunCompleted(output string, usage *openClawUsage) string {
	payload := map[string]any{
		"event": "run.completed", "run_id": "run_1", "output": output,
	}
	if usage != nil {
		payload["usage"] = usage
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

func sseRunFailed(reason string) string {
	b, _ := json.Marshal(map[string]any{
		"event": "run.failed", "run_id": "run_1", "reason": reason,
	})
	return string(b)
}

func TestHermesClient_RunTurn_Basic(t *testing.T) {
	stub, ts := newHermesRunStub("run_abc", []string{
		sseToolStarted("terminal", "echo hello"),
		sseToolCompleted("terminal", 0.317, false),
		sseDelta("The output is:"),
		sseDelta(" hello"),
		sseRunCompleted("The output is: hello", &openClawUsage{
			PromptTokens: 100, CompletionTokens: 5, TotalTokens: 105,
		}),
	})
	defer ts.Close()

	c := &HermesClient{
		GatewayURL: ts.URL,
		Token:      "hermes-test-key",
		Model:      "hermes-agent",
	}
	resp, err := c.RunTurn(context.Background(), TurnRequest{
		SessionID: "sess-1",
		Agent:     AgentSnapshot{SystemPrompt: "Be helpful."},
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"run echo hello"}]}`),
		},
	})
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	mustFindToolUse(t, resp.Events, "terminal")
	mustFindToolResult(t, resp.Events, "terminal", "(completed in 0.317s)")
	msg := mustFindAgentMessage(t, resp.Events)
	text := firstAgentText(t, msg)
	if text != "The output is: hello" {
		t.Errorf("final text=%q", text)
	}
	mustFindUsageSpan(t, resp.Events, "hermes", "hermes-agent")

	if resp.Usage == nil {
		t.Fatal("expected resp.Usage to be populated")
	}
	if resp.Usage.InputTokens != 100 {
		t.Errorf("InputTokens=%d want 100", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 5 {
		t.Errorf("OutputTokens=%d want 5", resp.Usage.OutputTokens)
	}

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.lastAuth != "Bearer hermes-test-key" {
		t.Errorf("auth=%q", stub.lastAuth)
	}
	var body map[string]any
	if err := json.Unmarshal(stub.lastBody, &body); err != nil {
		t.Fatal(err)
	}
	if body["session_id"] != "sess-1" {
		t.Errorf("session_id=%v", body["session_id"])
	}
	if body["instructions"] != "Be helpful." {
		t.Errorf("instructions=%v", body["instructions"])
	}
	if body["input"] != "run echo hello" {
		t.Errorf("input=%v", body["input"])
	}
	if body["model"] != "hermes-agent" {
		t.Errorf("model=%v", body["model"])
	}
}

func TestHermesClient_RunTurn_HermesUsageNaming(t *testing.T) {
	// Hermes's run.completed event uses input_tokens / output_tokens
	// (NOT the OpenAI prompt_tokens / completion_tokens). Verify we
	// decode the Hermes naming correctly and surface it in both
	// resp.Usage and the span event.
	runCompleted := `{"event":"run.completed","run_id":"run_h","output":"ok",` +
		`"usage":{"input_tokens":42,"output_tokens":7,"total_tokens":49}}`
	_, ts := newHermesRunStub("run_h", []string{runCompleted})
	defer ts.Close()

	c := &HermesClient{GatewayURL: ts.URL, Token: "k"}
	resp, err := c.RunTurn(context.Background(), TurnRequest{
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"hi"}]}`),
		},
	})
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if resp.Usage == nil {
		t.Fatal("expected resp.Usage to be populated")
	}
	if resp.Usage.InputTokens != 42 {
		t.Errorf("InputTokens=%d want 42", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 7 {
		t.Errorf("OutputTokens=%d want 7", resp.Usage.OutputTokens)
	}
	// Span should carry the Hermes usage through to the aggregate pipeline.
	var span map[string]any
	for _, raw := range resp.Events {
		var ev map[string]any
		_ = json.Unmarshal(raw, &ev)
		if ev["type"] == "span.model_request_end" {
			span = ev
			break
		}
	}
	if span == nil {
		t.Fatal("no span.model_request_end event")
	}
	usage, ok := span["model_usage"].(map[string]any)
	if !ok {
		t.Fatalf("span.model_usage not a map: %v", span["model_usage"])
	}
	if got, _ := usage["input_tokens"].(float64); got != 42 {
		t.Errorf("span input_tokens=%v want 42", got)
	}
	if got, _ := usage["output_tokens"].(float64); got != 7 {
		t.Errorf("span output_tokens=%v want 7", got)
	}
}

func TestHermesClient_RunTurn_DefaultModel(t *testing.T) {
	stub, ts := newHermesRunStub("run_x", []string{
		sseRunCompleted("ok", nil),
	})
	defer ts.Close()

	c := &HermesClient{GatewayURL: ts.URL, Token: "k"}
	_, err := c.RunTurn(context.Background(), TurnRequest{
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"hi"}]}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	var body map[string]any
	_ = json.Unmarshal(stub.lastBody, &body)
	if body["model"] != "hermes-agent" {
		t.Errorf("default model=%v want hermes-agent", body["model"])
	}
}

func TestHermesClient_RunTurn_NoUserMessage_FallsBackToContinue(t *testing.T) {
	stub, ts := newHermesRunStub("run_y", []string{
		sseRunCompleted("ok", nil),
	})
	defer ts.Close()

	c := &HermesClient{GatewayURL: ts.URL, Token: "k"}
	_, err := c.RunTurn(context.Background(), TurnRequest{
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"session.lifecycle","phase":"turn_start"}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	stub.mu.Lock()
	defer stub.mu.Unlock()
	var body map[string]any
	_ = json.Unmarshal(stub.lastBody, &body)
	if body["input"] != "(continue)" {
		t.Errorf("input=%v want (continue)", body["input"])
	}
}

func TestHermesClient_RunTurn_Failed(t *testing.T) {
	_, ts := newHermesRunStub("run_fail", []string{
		sseToolStarted("terminal", "rm -rf /"),
		sseRunFailed("tool rejected"),
	})
	defer ts.Close()

	c := &HermesClient{GatewayURL: ts.URL, Token: "k"}
	_, err := c.RunTurn(context.Background(), TurnRequest{
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"go"}]}`),
		},
	})
	if err == nil {
		t.Fatal("expected error for run.failed")
	}
	if !strings.Contains(err.Error(), "tool rejected") {
		t.Errorf("err=%v", err)
	}
}

func TestHermesClient_RunTurn_CreateHTTPError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
	}))
	defer ts.Close()

	c := &HermesClient{GatewayURL: ts.URL, Token: "k"}
	_, err := c.RunTurn(context.Background(), TurnRequest{
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"hi"}]}`),
		},
	})
	if err == nil {
		t.Fatal("expected error for 500")
	}
	if !strings.Contains(err.Error(), "status=500") {
		t.Errorf("err=%v", err)
	}
}

func TestHermesClient_RunTurnStream_EmitsProgressively(t *testing.T) {
	_, ts := newHermesRunStub("run_s", []string{
		sseToolStarted("terminal", "ls"),
		sseToolCompleted("terminal", 0.1, false),
		sseDelta("first "),
		sseDelta("second"),
		sseRunCompleted("first second", &openClawUsage{
			PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12,
		}),
	})
	defer ts.Close()

	c := &HermesClient{GatewayURL: ts.URL, Token: "k"}
	var events []json.RawMessage
	err := c.RunTurnStream(context.Background(), TurnRequest{
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"go"}]}`),
		},
	}, func(ev json.RawMessage) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("RunTurnStream: %v", err)
	}

	// Order: tool_use → tool_result → agent.message → agent.message → span.model_request_end
	types := make([]string, 0, len(events))
	for _, raw := range events {
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		types = append(types, m["type"].(string))
	}
	want := []string{
		"agent.tool_use",
		"agent.tool_result",
		"agent.message",
		"agent.message",
		"span.model_request_end",
	}
	if len(types) != len(want) {
		t.Fatalf("events=%v want %v", types, want)
	}
	for i, w := range want {
		if types[i] != w {
			t.Errorf("event[%d]=%q want %q", i, types[i], w)
		}
	}

	// Final agent.message should carry the accumulated text.
	var lastAgent map[string]any
	for i := len(events) - 1; i >= 0; i-- {
		var ev map[string]any
		_ = json.Unmarshal(events[i], &ev)
		if ev["type"] == "agent.message" {
			lastAgent = ev
			break
		}
	}
	if firstAgentText(t, lastAgent) != "first second" {
		t.Errorf("final text=%q", firstAgentText(t, lastAgent))
	}
}

func TestHermesClient_RunTurnStream_ToolFailed_MarksResultFailed(t *testing.T) {
	_, ts := newHermesRunStub("run_e", []string{
		sseToolStarted("terminal", "fail"),
		sseToolCompleted("terminal", 0.05, true),
		sseRunCompleted("error happened", nil),
	})
	defer ts.Close()

	c := &HermesClient{GatewayURL: ts.URL, Token: "k"}
	var events []json.RawMessage
	err := c.RunTurnStream(context.Background(), TurnRequest{
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"go"}]}`),
		},
	}, func(ev json.RawMessage) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("RunTurnStream: %v", err)
	}
	mustFindToolResult(t, events, "terminal", "(failed)")
}

func TestHermesClient_RunTurnStream_NoDeltasEmitsOutput(t *testing.T) {
	// Some runs may complete with a final output but no deltas
	// (e.g. a tool-only turn that produced no assistant text until
	// the final answer). We should still see one agent.message.
	_, ts := newHermesRunStub("run_nod", []string{
		sseRunCompleted("final answer", nil),
	})
	defer ts.Close()

	c := &HermesClient{GatewayURL: ts.URL, Token: "k"}
	var events []json.RawMessage
	err := c.RunTurnStream(context.Background(), TurnRequest{
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"go"}]}`),
		},
	}, func(ev json.RawMessage) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("RunTurnStream: %v", err)
	}
	msg := mustFindAgentMessage(t, events)
	if firstAgentText(t, msg) != "final answer" {
		t.Errorf("text=%q", firstAgentText(t, msg))
	}
}

// --- helpers ---------------------------------------------------------------

func mustFindAgentMessage(t *testing.T, events []json.RawMessage) map[string]any {
	t.Helper()
	// Return the LAST agent.message — the Runs API emits an
	// agent.message per delta, each carrying the full accumulated
	// text, so the last one is the final answer.
	var found map[string]any
	for _, raw := range events {
		var ev map[string]any
		_ = json.Unmarshal(raw, &ev)
		if ev["type"] == "agent.message" {
			found = ev
		}
	}
	if found == nil {
		t.Fatalf("no agent.message event in %d events", len(events))
	}
	return found
}

func firstAgentText(t *testing.T, ev map[string]any) string {
	t.Helper()
	content, ok := ev["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("no content in event: %+v", ev)
	}
	first, ok := content[0].(map[string]any)
	if !ok {
		t.Fatalf("content[0] not a map: %+v", content[0])
	}
	text, _ := first["text"].(string)
	return text
}

func mustFindToolUse(t *testing.T, events []json.RawMessage, name string) map[string]any {
	t.Helper()
	for _, raw := range events {
		var ev map[string]any
		_ = json.Unmarshal(raw, &ev)
		if ev["type"] == "agent.tool_use" && ev["name"] == name {
			return ev
		}
	}
	t.Fatalf("no agent.tool_use{name=%q} in %d events", name, len(events))
	return nil
}

func mustFindToolResult(t *testing.T, events []json.RawMessage, name, content string) map[string]any {
	t.Helper()
	for _, raw := range events {
		var ev map[string]any
		_ = json.Unmarshal(raw, &ev)
		if ev["type"] != "agent.tool_result" || ev["name"] != name {
			continue
		}
		if got, _ := ev["content"].(string); got != content {
			t.Errorf("tool_result content=%q want %q", got, content)
		}
		return ev
	}
	t.Fatalf("no agent.tool_result{name=%q, content=%q} in %d events", name, content, len(events))
	return nil
}

func mustFindUsageSpan(t *testing.T, events []json.RawMessage, provider, model string) {
	t.Helper()
	for _, raw := range events {
		var ev map[string]any
		_ = json.Unmarshal(raw, &ev)
		if ev["type"] != "span.model_request_end" {
			continue
		}
		if ev["provider"] != provider {
			t.Errorf("span provider=%v want %v", ev["provider"], provider)
		}
		if ev["model"] != model {
			t.Errorf("span model=%v want %v", ev["model"], model)
		}
		if _, ok := ev["model_usage"]; !ok {
			t.Error("span missing model_usage")
		}
		if _, ok := ev["duration_ms"]; !ok {
			t.Error("span missing duration_ms")
		}
		return
	}
	t.Fatalf("no span.model_request_end found in %d events", len(events))
}
