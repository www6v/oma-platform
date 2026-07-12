package harness

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// hermesStub records the last request and serves canned responses.
type hermesStub struct {
	mu        sync.Mutex
	lastBody  []byte
	lastAuth  string
	lastModel string
}

func newHermesStub(responseContent string) (*hermesStub, *httptest.Server) {
	stub := &hermesStub{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.mu.Lock()
		stub.lastAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		stub.lastBody = body
		var req hermesChatRequest
		_ = json.Unmarshal(body, &req)
		stub.lastModel = req.Model
		stub.mu.Unlock()

		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}

		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			// Split response into two chunks.
			half := len(responseContent) / 2
			if half == 0 {
				half = len(responseContent)
			}
			chunks := []string{responseContent[:half], responseContent[half:]}
			for _, c := range chunks {
				chunk := hermesSSEChunk{}
				if len(chunk.Choices) == 0 {
					chunk.Choices = make([]struct {
						Delta struct {
							Content string `json:"content"`
						} `json:"delta"`
						FinishReason *string `json:"finish_reason"`
					}, 1)
				}
				chunk.Choices[0].Delta.Content = c
				data, _ := json.Marshal(chunk)
				_, _ = w.Write([]byte("data: "))
				_, _ = w.Write(data)
				_, _ = w.Write([]byte("\n\n"))
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			}
			_, _ = w.Write([]byte("data: [DONE]\n\n"))
			return
		}

		resp := hermesChatResponse{}
		resp.Choices = make([]struct {
			Message struct {
				Role    string `json:"role"`
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		}, 1)
		resp.Choices[0].Message.Role = "assistant"
		resp.Choices[0].Message.Content = responseContent
		resp.Choices[0].FinishReason = "stop"
		// Stub usage so tests can assert TurnResponse.Usage is
		// populated. Real Hermes always returns this.
		resp.Usage = &openClawUsage{
			PromptTokens:     42,
			CompletionTokens: 7,
			TotalTokens:      49,
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	return stub, ts
}

func TestHermesClient_RunTurn_Basic(t *testing.T) {
	stub, ts := newHermesStub("hello from hermes")
	defer ts.Close()

	c := &HermesClient{
		GatewayURL: ts.URL,
		Token:      "hermes-test-key",
		Model:      "hermes-agent",
	}
	resp, err := c.RunTurn(context.Background(), TurnRequest{
		SessionID: "sess-1",
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"hi"}]}`),
		},
	})
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	agentEv := mustFindAgentMessage(t, resp.Events)
	content := agentEv["content"].([]any)
	first := content[0].(map[string]any)
	if first["text"] != "hello from hermes" {
		t.Errorf("text=%q", first["text"])
	}
	// Usage span should also be present.
	mustFindUsageSpan(t, resp.Events, "hermes", "hermes-agent")
	// TurnResponse.Usage should be populated from upstream usage.
	if resp.Usage == nil {
		t.Fatal("expected resp.Usage to be populated")
	}
	if resp.Usage.InputTokens != 42 {
		t.Errorf("InputTokens=%d want 42", resp.Usage.InputTokens)
	}
	if resp.Usage.OutputTokens != 7 {
		t.Errorf("OutputTokens=%d want 7", resp.Usage.OutputTokens)
	}

	stub.mu.Lock()
	defer stub.mu.Unlock()
	if stub.lastAuth != "Bearer hermes-test-key" {
		t.Errorf("auth=%q", stub.lastAuth)
	}
	if stub.lastModel != "hermes-agent" {
		t.Errorf("model=%q", stub.lastModel)
	}
}

func TestHermesClient_RunTurn_DefaultModel(t *testing.T) {
	stub, ts := newHermesStub("ok")
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
	if stub.lastModel != "hermes-agent" {
		t.Errorf("default model=%q want hermes-agent", stub.lastModel)
	}
}

func TestHermesClient_RunTurn_SendsFullHistory(t *testing.T) {
	stub, ts := newHermesStub("ok")
	defer ts.Close()

	c := &HermesClient{GatewayURL: ts.URL, Token: "k"}
	_, err := c.RunTurn(context.Background(), TurnRequest{
		Agent: AgentSnapshot{SystemPrompt: "Be brief."},
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"first"}]}`),
			json.RawMessage(`{"type":"agent.message","id":"a1","content":[{"type":"text","text":"response1"}]}`),
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"second"}]}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	stub.mu.Lock()
	defer stub.mu.Unlock()
	var req hermesChatRequest
	if err := json.Unmarshal(stub.lastBody, &req); err != nil {
		t.Fatal(err)
	}
	// Expected: system + user:first + assistant:response1 + user:second = 4
	if len(req.Messages) != 4 {
		t.Fatalf("messages=%d want 4: %+v", len(req.Messages), req.Messages)
	}
	if req.Messages[0].Role != "system" || req.Messages[0].Content != "Be brief." {
		t.Errorf("msg[0]=%+v", req.Messages[0])
	}
	if req.Messages[1].Role != "user" || req.Messages[1].Content != "first" {
		t.Errorf("msg[1]=%+v", req.Messages[1])
	}
	if req.Messages[2].Role != "assistant" || req.Messages[2].Content != "response1" {
		t.Errorf("msg[2]=%+v", req.Messages[2])
	}
	if req.Messages[3].Role != "user" || req.Messages[3].Content != "second" {
		t.Errorf("msg[3]=%+v", req.Messages[3])
	}
}

func TestHermesClient_RunTurn_NoUserMessage_AppendsContinue(t *testing.T) {
	stub, ts := newHermesStub("ok")
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
	var req hermesChatRequest
	_ = json.Unmarshal(stub.lastBody, &req)
	if len(req.Messages) != 1 || req.Messages[0].Role != "user" || req.Messages[0].Content != "(continue)" {
		t.Errorf("expected fallback (continue) message, got %+v", req.Messages)
	}
}

func TestHermesClient_RunTurn_HTTPError(t *testing.T) {
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

func TestHermesClient_RunTurnStream(t *testing.T) {
	_, ts := newHermesStub("hello world")
	defer ts.Close()

	c := &HermesClient{GatewayURL: ts.URL, Token: "k"}
	var events []json.RawMessage
	err := c.RunTurnStream(context.Background(), TurnRequest{
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"hi"}]}`),
		},
	}, func(ev json.RawMessage) error {
		events = append(events, ev)
		return nil
	})
	if err != nil {
		t.Fatalf("RunTurnStream: %v", err)
	}
	if len(events) == 0 {
		t.Fatal("no events emitted")
	}
	// Find the last agent.message event — a span.model_request_end is
	// now appended after the content events.
	var lastAgent map[string]any
	for i := len(events) - 1; i >= 0; i-- {
		var ev map[string]any
		_ = json.Unmarshal(events[i], &ev)
		if ev["type"] == "agent.message" {
			lastAgent = ev
			break
		}
	}
	if lastAgent == nil {
		t.Fatal("no agent.message event in stream")
	}
	content := lastAgent["content"].([]any)
	first := content[0].(map[string]any)
	if first["text"] != "hello world" {
		t.Errorf("final text=%q", first["text"])
	}
	// Also verify the usage span was emitted.
	var sawSpan bool
	for _, ev := range events {
		var m map[string]any
		_ = json.Unmarshal(ev, &m)
		if m["type"] == "span.model_request_end" {
			sawSpan = true
			if m["provider"] != "hermes" {
				t.Errorf("span provider=%v want hermes", m["provider"])
			}
			if m["model"] != "hermes-agent" {
				t.Errorf("span model=%v want hermes-agent", m["model"])
			}
		}
	}
	if !sawSpan {
		t.Error("expected span.model_request_end event in stream")
	}
}

func TestEventsToHermesMessages(t *testing.T) {
	events := []json.RawMessage{
		json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"hello"}]}`),
		json.RawMessage(`{"type":"session.lifecycle","phase":"turn_start"}`), // skipped
		json.RawMessage(`{"type":"agent.message","id":"a1","content":[{"type":"text","text":"hi back"}]}`),
		json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"how"},{"type":"text","text":" are you"}]}`),
	}
	got := eventsToHermesMessages(events)
	if len(got) != 3 {
		t.Fatalf("len=%d want 3: %+v", len(got), got)
	}
	if got[0].Role != "user" || got[0].Content != "hello" {
		t.Errorf("msg[0]=%+v", got[0])
	}
	if got[1].Role != "assistant" || got[1].Content != "hi back" {
		t.Errorf("msg[1]=%+v", got[1])
	}
	if got[2].Role != "user" || got[2].Content != "how are you" {
		t.Errorf("msg[2]=%+v", got[2])
	}
}

func TestHasRole(t *testing.T) {
	msgs := []hermesMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "u"},
	}
	if !hasRole(msgs, "user") {
		t.Error("expected user present")
	}
	if !hasRole(msgs, "system") {
		t.Error("expected system present")
	}
	if hasRole(msgs, "assistant") {
		t.Error("expected assistant absent")
	}
}

// mustFindAgentMessage returns the first agent.message event parsed as
// a map. Fails the test if none found.
func mustFindAgentMessage(t *testing.T, events []json.RawMessage) map[string]any {
	t.Helper()
	for _, raw := range events {
		var ev map[string]any
		_ = json.Unmarshal(raw, &ev)
		if ev["type"] == "agent.message" {
			return ev
		}
	}
	t.Fatalf("no agent.message event found in %d events", len(events))
	return nil
}

// mustFindUsageSpan asserts that a span.model_request_end event is
// present with the expected provider and model fields.
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
