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

// --- OpenResponses stub server ---------------------------------------------

// orStub is a minimal OpenResponses-API test server. It accepts
// POST /v1/responses and serves the supplied SSE lines as the response
// body. Also records the request for assertions.
type orStubRequest struct {
	Model        string `json:"model"`
	Input        string `json:"input"`
	Instructions string `json:"instructions"`
	Stream       bool   `json:"stream"`
	SessionKey   string
	AuthHeader   string
}

type orStub struct {
	*httptest.Server

	mu       sync.Mutex
	requests []orStubRequest
	// events is the list of raw JSON objects served as SSE data lines.
	events []string
	// status overrides the POST /v1/responses status code.
	status int
	// body overrides the POST /v1/responses response body (non-streaming).
	body string
}

func newORStub(events []string) *orStub {
	s := &orStub{events: events}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/responses" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req orStubRequest
		_ = json.Unmarshal(body, &req)
		req.SessionKey = r.Header.Get("x-openclaw-session-key")
		req.AuthHeader = r.Header.Get("Authorization")

		s.mu.Lock()
		s.requests = append(s.requests, req)
		s.mu.Unlock()

		if s.status != 0 {
			w.WriteHeader(s.status)
			if s.body != "" {
				_, _ = w.Write([]byte(s.body))
			}
			return
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, ev := range s.events {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", ev)
			flusher.Flush()
		}
		_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	return s
}

func (s *orStub) lastRequest() orStubRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.requests) == 0 {
		return orStubRequest{}
	}
	return s.requests[len(s.requests)-1]
}

// --- SSE helpers -----------------------------------------------------------

func orCreated() string {
	b, _ := json.Marshal(map[string]any{
		"type":     "response.created",
		"response": map[string]any{"id": "resp_1", "status": "in_progress"},
	})
	return string(b)
}

func orInProgress() string {
	b, _ := json.Marshal(map[string]any{
		"type":     "response.in_progress",
		"response": map[string]any{"id": "resp_1", "status": "in_progress"},
	})
	return string(b)
}

func orOutputItemAddedFunctionCall(name, args string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "response.output_item.added", "output_index": 0,
		"item": map[string]any{
			"type": "function_call", "id": "fc_1", "call_id": "call_1",
			"name": name, "arguments": args,
		},
	})
	return string(b)
}

func orOutputItemDoneFunctionCall(name, args string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "response.output_item.done", "output_index": 0,
		"item": map[string]any{
			"type": "function_call", "id": "fc_1", "call_id": "call_1",
			"name": name, "arguments": args,
		},
	})
	return string(b)
}

func orOutputItemAddedMessage() string {
	b, _ := json.Marshal(map[string]any{
		"type": "response.output_item.added", "output_index": 1,
		"item": map[string]any{"type": "message", "id": "msg_1", "role": "assistant"},
	})
	return string(b)
}

func orContentPartAdded() string {
	b, _ := json.Marshal(map[string]any{
		"type": "response.content_part.added", "output_index": 1, "content_index": 0,
		"part": map[string]any{"type": "output_text"},
	})
	return string(b)
}

func orTextDelta(delta string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "response.output_text.delta",
		"output_index": 1, "content_index": 0, "delta": delta,
	})
	return string(b)
}

func orTextDone(text string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "response.output_text.done",
		"output_index": 1, "content_index": 0, "text": text,
	})
	return string(b)
}

func orContentPartDone() string {
	b, _ := json.Marshal(map[string]any{
		"type": "response.content_part.done", "output_index": 1, "content_index": 0,
	})
	return string(b)
}

func orOutputItemDoneMessage() string {
	b, _ := json.Marshal(map[string]any{
		"type": "response.output_item.done", "output_index": 1,
		"item": map[string]any{"type": "message", "id": "msg_1", "role": "assistant"},
	})
	return string(b)
}

func orCompleted(usage *openClawUsage) string {
	payload := map[string]any{
		"type": "response.completed",
		"response": map[string]any{
			"id": "resp_1", "status": "completed",
			"output": []any{},
		},
	}
	if usage != nil {
		resp := payload["response"].(map[string]any)
		resp["usage"] = usage
	}
	b, _ := json.Marshal(payload)
	return string(b)
}

func orFailed(msg string) string {
	b, _ := json.Marshal(map[string]any{
		"type": "response.failed",
		"response": map[string]any{
			"id": "resp_1", "status": "failed",
			"error": map[string]any{"type": "server_error", "message": msg},
		},
	})
	return string(b)
}

// --- full-event fixture (tool + text + usage) ---

func fullFixture() []string {
	return []string{
		orCreated(),
		orInProgress(),
		orOutputItemAddedFunctionCall("terminal", `{"command":"echo hello"}`),
		orOutputItemDoneFunctionCall("terminal", `{"command":"echo hello"}`),
		orOutputItemAddedMessage(),
		orContentPartAdded(),
		orTextDelta("The output is:"),
		orTextDelta(" hello"),
		orTextDone("The output is: hello"),
		orContentPartDone(),
		orOutputItemDoneMessage(),
		orCompleted(&openClawUsage{
			PromptTokens: 50, CompletionTokens: 10, TotalTokens: 60,
		}),
	}
}

// --- Tests -----------------------------------------------------------------

func TestOpenClawClient_RunTurn_Basic(t *testing.T) {
	stub := newORStub(fullFixture())
	defer stub.Close()

	client := &OpenClawClient{
		GatewayURL: stub.URL,
		Token:      "test-token",
		Agent:      "openclaw/default",
	}

	resp, err := client.RunTurn(context.Background(), TurnRequest{
		SessionID: "sess-1",
		Agent:     AgentSnapshot{SystemPrompt: "You are helpful."},
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"run echo hello"}]}`),
		},
	})
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	// Should have tool_use, tool_result, agent.message(s), span.model_request_end
	mustFindToolUse(t, resp.Events, "terminal")
	mustFindToolResult(t, resp.Events, "terminal", "(completed)")
	msg := mustFindAgentMessage(t, resp.Events)
	text := firstAgentText(t, msg)
	if text != "The output is: hello" {
		t.Errorf("final text=%q", text)
	}
	mustFindUsageSpan(t, resp.Events, "openclaw", "openclaw/default")

	if resp.Usage == nil {
		t.Fatal("expected resp.Usage to be populated")
	}
	if resp.Usage.InputTokens != 50 {
		t.Errorf("InputTokens=%d want 50", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 10 {
		t.Errorf("OutputTokens=%d want 10", resp.Usage.OutputTokens)
	}

	// Verify request body.
	got := stub.lastRequest()
	if got.Model != "openclaw/default" {
		t.Errorf("model=%q want openclaw/default", got.Model)
	}
	if got.Input != "run echo hello" {
		t.Errorf("input=%q", got.Input)
	}
	if got.Instructions != "You are helpful." {
		t.Errorf("instructions=%q", got.Instructions)
	}
	if !got.Stream {
		t.Error("expected stream=true")
	}
	if got.AuthHeader != "Bearer test-token" {
		t.Errorf("auth=%q want Bearer test-token", got.AuthHeader)
	}
	if got.SessionKey != "oma-sess-1" {
		t.Errorf("sessionKey=%q want oma-sess-1", got.SessionKey)
	}
}

func TestOpenClawClient_RunTurn_TextOnly(t *testing.T) {
	// No function_call — just text deltas + usage.
	stub := newORStub([]string{
		orCreated(),
		orInProgress(),
		orOutputItemAddedMessage(),
		orContentPartAdded(),
		orTextDelta("hello "),
		orTextDelta("world"),
		orTextDone("hello world"),
		orContentPartDone(),
		orOutputItemDoneMessage(),
		orCompleted(&openClawUsage{
			PromptTokens: 10, CompletionTokens: 2, TotalTokens: 12,
		}),
	})
	defer stub.Close()

	client := &OpenClawClient{GatewayURL: stub.URL, Token: "t"}
	resp, err := client.RunTurn(context.Background(), TurnRequest{
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"hi"}]}`),
		},
	})
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	// Should NOT have tool events.
	for _, raw := range resp.Events {
		var ev map[string]any
		_ = json.Unmarshal(raw, &ev)
		if ev["type"] == "agent.tool_use" || ev["type"] == "agent.tool_result" {
			t.Errorf("unexpected tool event: %v", ev["type"])
		}
	}

	msg := mustFindAgentMessage(t, resp.Events)
	if firstAgentText(t, msg) != "hello world" {
		t.Errorf("text=%q", firstAgentText(t, msg))
	}
	if resp.Usage == nil || resp.Usage.InputTokens != 10 {
		t.Errorf("usage=%+v", resp.Usage)
	}
}

func TestOpenClawClient_RunTurnStream_EmitsProgressively(t *testing.T) {
	stub := newORStub(fullFixture())
	defer stub.Close()

	client := &OpenClawClient{GatewayURL: stub.URL, Token: "t"}
	var events []json.RawMessage
	err := client.RunTurnStream(context.Background(), TurnRequest{
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

	// Expected order: tool_use → tool_result → agent.message → ... → span.model_request_end
	types := make([]string, 0, len(events))
	for _, raw := range events {
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		types = append(types, m["type"].(string))
	}
	// Verify: tool_use first, then tool_result, then at least one agent.message, then span.
	if len(types) < 4 {
		t.Fatalf("events=%v want at least 4", types)
	}
	if types[0] != "agent.tool_use" {
		t.Errorf("first event=%q want agent.tool_use", types[0])
	}
	if types[1] != "agent.tool_result" {
		t.Errorf("second event=%q want agent.tool_result", types[1])
	}
	if types[len(types)-1] != "span.model_request_end" {
		t.Errorf("last event=%q want span.model_request_end", types[len(types)-1])
	}
	// Middle events should be agent.message.
	for i := 2; i < len(types)-1; i++ {
		if types[i] != "agent.message" {
			t.Errorf("event[%d]=%q want agent.message", i, types[i])
		}
	}
}

func TestOpenClawClient_RunTurn_PassesInstructionsAndSession(t *testing.T) {
	stub := newORStub([]string{orCompleted(nil)})
	defer stub.Close()

	client := &OpenClawClient{GatewayURL: stub.URL, Token: "t"}
	_, err := client.RunTurn(context.Background(), TurnRequest{
		SessionID: "sess-xyz",
		Agent:     AgentSnapshot{SystemPrompt: "Be terse."},
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"hi"}]}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := stub.lastRequest()
	if got.Instructions != "Be terse." {
		t.Errorf("instructions=%q", got.Instructions)
	}
	if got.SessionKey != "oma-sess-xyz" {
		t.Errorf("sessionKey=%q want oma-sess-xyz", got.SessionKey)
	}
	if got.Input != "hi" {
		t.Errorf("input=%q want hi", got.Input)
	}
}

func TestOpenClawClient_RunTurn_DefaultModel(t *testing.T) {
	stub := newORStub([]string{orCompleted(nil)})
	defer stub.Close()

	client := &OpenClawClient{GatewayURL: stub.URL, Token: "t"}
	// Agent is empty — should fall back to "openclaw/default".
	_, err := client.RunTurn(context.Background(), TurnRequest{
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"hi"}]}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := stub.lastRequest().Model; got != "openclaw/default" {
		t.Errorf("model=%q want openclaw/default", got)
	}
}

func TestOpenClawClient_RunTurn_NoUserMessage(t *testing.T) {
	stub := newORStub([]string{orCompleted(nil)})
	defer stub.Close()

	client := &OpenClawClient{GatewayURL: stub.URL, Token: "t"}
	_, err := client.RunTurn(context.Background(), TurnRequest{
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"session.lifecycle","phase":"turn_start"}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := stub.lastRequest()
	if got.Input != "(continue)" {
		t.Errorf("input=%q want (continue)", got.Input)
	}
}

func TestOpenClawClient_RunTurn_Failed(t *testing.T) {
	stub := newORStub([]string{
		orCreated(),
		orFailed("tool rejected"),
	})
	defer stub.Close()

	client := &OpenClawClient{GatewayURL: stub.URL, Token: "t"}
	_, err := client.RunTurn(context.Background(), TurnRequest{
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"go"}]}`),
		},
	})
	if err == nil {
		t.Fatal("expected error for response.failed")
	}
	if !strings.Contains(err.Error(), "tool rejected") {
		t.Errorf("err=%v", err)
	}
}

func TestOpenClawClient_RunTurn_HTTPError(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"boom"}`, http.StatusInternalServerError)
	}))
	defer stub.Close()

	client := &OpenClawClient{GatewayURL: stub.URL, Token: "t"}
	_, err := client.RunTurn(context.Background(), TurnRequest{
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"hi"}]}`),
		},
	})
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("err=%v should mention status", err)
	}
}

func TestOpenClawClient_RunTurn_NoDeltasEmitsResponseOutput(t *testing.T) {
	// Tool-only run with a final assembled output but no text deltas.
	// response.completed carries output[0].content[0].text.
	completed := `{"type":"response.completed","response":{` +
		`"id":"resp_1","status":"completed",` +
		`"output":[{"type":"message","role":"assistant",` +
		`"content":[{"type":"output_text","text":"final answer"}]}],` +
		`"usage":{"input_tokens":5,"output_tokens":2,"total_tokens":7}}}`
	stub := newORStub([]string{
		orCreated(),
		orOutputItemAddedFunctionCall("terminal", "ls"),
		orOutputItemDoneFunctionCall("terminal", "ls"),
		completed,
	})
	defer stub.Close()

	client := &OpenClawClient{GatewayURL: stub.URL, Token: "t"}
	resp, err := client.RunTurn(context.Background(), TurnRequest{
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"go"}]}`),
		},
	})
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	msg := mustFindAgentMessage(t, resp.Events)
	if firstAgentText(t, msg) != "final answer" {
		t.Errorf("text=%q want 'final answer'", firstAgentText(t, msg))
	}
	if resp.Usage == nil || resp.Usage.InputTokens != 5 {
		t.Errorf("usage=%+v", resp.Usage)
	}
}

// --- ExtractLastUserMessage tests (still relevant) -------------------------

func TestExtractLastUserMessage(t *testing.T) {
	tests := []struct {
		name   string
		events []json.RawMessage
		want   string
	}{
		{
			name:   "empty",
			events: nil,
			want:   "",
		},
		{
			name: "single user message",
			events: []json.RawMessage{
				json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"hello"}]}`),
			},
			want: "hello",
		},
		{
			name: "last of multiple",
			events: []json.RawMessage{
				json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"first"}]}`),
				json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"second"}]}`),
			},
			want: "second",
		},
		{
			name: "ignores non-user events",
			events: []json.RawMessage{
				json.RawMessage(`{"type":"session.lifecycle","phase":"turn_start"}`),
				json.RawMessage(`{"type":"agent.message","id":"a","content":[{"type":"text","text":"bot"}]}`),
				json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"found"}]}`),
			},
			want: "found",
		},
		{
			name: "no text blocks",
			events: []json.RawMessage{
				json.RawMessage(`{"type":"user.message","content":[{"type":"image"}]}`),
			},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractLastUserMessage(tt.events)
			if got != tt.want {
				t.Errorf("extractLastUserMessage()=%q want %q", got, tt.want)
			}
		})
	}
}
