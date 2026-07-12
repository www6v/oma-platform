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

// stubOpenClawServer spins up an httptest server that mimics the OpenClaw
// /v1/chat/completions endpoint. The returned recorder captures requests
// for assertion.
type stubRequest struct {
	Model       string            `json:"model"`
	Messages    []openClawMessage `json:"messages"`
	Stream      bool              `json:"stream"`
	SessionKey  string
	AuthHeader  string
}

type stubServer struct {
	*httptest.Server

	mu       sync.Mutex
	requests []stubRequest
	// reply is the assistant content to return.
	reply string
}

func newStubServer(reply string) *stubServer {
	s := &stubServer{reply: reply}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(r.Body)
		var req stubRequest
		_ = json.Unmarshal(body, &req)
		req.SessionKey = r.Header.Get("x-openclaw-session-key")
		req.AuthHeader = r.Header.Get("Authorization")

		s.mu.Lock()
		s.requests = append(s.requests, req)
		s.mu.Unlock()

		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			flusher, _ := w.(http.Flusher)
			words := strings.Fields(s.reply)
			for i, word := range words {
				chunk := fmt.Sprintf(
					`{"choices":[{"delta":{"content":"%s"}}]}`,
					word,
				)
				if i < len(words)-1 {
					chunk = fmt.Sprintf(
						`{"choices":[{"delta":{"content":"%s "}}]}`,
						word,
					)
				}
				fmt.Fprintf(w, "data: %s\n\n", chunk)
				flusher.Flush()
			}
			fmt.Fprintf(w, "data: [DONE]\n\n")
			flusher.Flush()
			return
		}

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"choices":[{"message":{"role":"assistant","content":%q},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":17,"completion_tokens":3,"total_tokens":20}
		}`, s.reply)
	}))
	return s
}

func (s *stubServer) lastRequest() stubRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.requests) == 0 {
		return stubRequest{}
	}
	return s.requests[len(s.requests)-1]
}

func TestOpenClawClient_RunTurn_Basic(t *testing.T) {
	stub := newStubServer("hello from openclaw")
	defer stub.Close()

	client := &OpenClawClient{
		GatewayURL: stub.URL,
		Token:      "test-token",
		Agent:      "openclaw/default",
	}

	events := []json.RawMessage{
		json.RawMessage(`{"type":"session.lifecycle","phase":"turn_start"}`),
		json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"hi there"}]}`),
	}

	resp, err := client.RunTurn(context.Background(), TurnRequest{
		SessionID: "sess-1",
		Agent:     AgentSnapshot{SystemPrompt: "You are helpful."},
		Events:    events,
	})
	if err != nil {
		t.Fatalf("RunTurn: %v", err)
	}
	if len(resp.Events) < 1 {
		t.Fatalf("events=%d want at least 1", len(resp.Events))
	}

	var ev map[string]any
	if err := json.Unmarshal(resp.Events[0], &ev); err != nil {
		t.Fatal(err)
	}
	if ev["type"] != "agent.message" {
		t.Errorf("type=%v want agent.message", ev["type"])
	}

	// Verify request body.
	got := stub.lastRequest()
	if got.Model != "openclaw/default" {
		t.Errorf("model=%q want openclaw/default", got.Model)
	}
	if len(got.Messages) != 2 {
		t.Fatalf("messages=%d want 2 (system + user)", len(got.Messages))
	}
	if got.Messages[0].Role != "system" || got.Messages[0].Content != "You are helpful." {
		t.Errorf("system msg=%+v", got.Messages[0])
	}
	if got.Messages[1].Role != "user" || got.Messages[1].Content != "hi there" {
		t.Errorf("user msg=%+v", got.Messages[1])
	}
	if got.AuthHeader != "Bearer test-token" {
		t.Errorf("auth=%q want Bearer test-token", got.AuthHeader)
	}
	// Usage span should be in the events and TurnResponse.Usage populated.
	if resp.Usage == nil {
		t.Fatal("expected resp.Usage to be populated")
	}
	if resp.Usage.InputTokens != 17 || resp.Usage.OutputTokens != 3 {
		t.Errorf("usage=%+v want {17, 3}", resp.Usage)
	}
	foundSpan := false
	for _, raw := range resp.Events {
		var e map[string]any
		_ = json.Unmarshal(raw, &e)
		if e["type"] == "span.model_request_end" {
			foundSpan = true
			if e["provider"] != "openclaw" {
				t.Errorf("span provider=%v want openclaw", e["provider"])
			}
		}
	}
	if !foundSpan {
		t.Error("expected span.model_request_end in events")
	}
	if got.SessionKey != "oma-sess-1" {
		t.Errorf("sessionKey=%q want oma-sess-1", got.SessionKey)
	}
}

func TestOpenClawClient_RunTurn_DefaultModel(t *testing.T) {
	stub := newStubServer("ok")
	defer stub.Close()

	client := &OpenClawClient{GatewayURL: stub.URL, Token: "t"}
	// Agent is empty — should fall back to "openclaw/default".

	resp, err := client.RunTurn(context.Background(), TurnRequest{
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"hi"}]}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Events) < 1 {
		t.Fatalf("events=%d want at least 1", len(resp.Events))
	}
	if got := stub.lastRequest().Model; got != "openclaw/default" {
		t.Errorf("model=%q want openclaw/default", got)
	}
}

func TestOpenClawClient_RunTurn_NoUserMessage(t *testing.T) {
	stub := newStubServer("fallback")
	defer stub.Close()

	client := &OpenClawClient{GatewayURL: stub.URL, Token: "t"}

	resp, err := client.RunTurn(context.Background(), TurnRequest{
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"session.lifecycle","phase":"turn_start"}`),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Events) < 1 {
		t.Fatalf("events=%d want at least 1", len(resp.Events))
	}
	// Should fall back to "(continue)" when no user.message found.
	got := stub.lastRequest()
	if got.Messages[0].Content != "(continue)" {
		t.Errorf("fallback content=%q want (continue)", got.Messages[0].Content)
	}
}

func TestOpenClawClient_RunTurn_ExtractsLastUserMessage(t *testing.T) {
	stub := newStubServer("ok")
	defer stub.Close()

	client := &OpenClawClient{GatewayURL: stub.URL, Token: "t"}

	events := []json.RawMessage{
		json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"first"}]}`),
		json.RawMessage(`{"type":"agent.message","id":"a1","content":[{"type":"text","text":"reply"}]}`),
		json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"second"}]}`),
	}

	_, err := client.RunTurn(context.Background(), TurnRequest{Events: events})
	if err != nil {
		t.Fatal(err)
	}
	got := stub.lastRequest()
	if got.Messages[0].Content != "second" {
		t.Errorf("should extract LAST user message, got %q", got.Messages[0].Content)
	}
}

func TestOpenClawClient_RunTurn_HTTPError(t *testing.T) {
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprint(w, "internal error")
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
		t.Errorf("error should mention status code: %v", err)
	}
}

func TestOpenClawClient_RunTurnStream(t *testing.T) {
	stub := newStubServer("one two three")
	defer stub.Close()

	client := &OpenClawClient{GatewayURL: stub.URL, Token: "t"}
	var received []string
	err := client.RunTurnStream(context.Background(), TurnRequest{
		SessionID: "sess-stream",
		Events: []json.RawMessage{
			json.RawMessage(`{"type":"user.message","content":[{"type":"text","text":"count"}]}`),
		},
	}, func(event json.RawMessage) error {
		var ev map[string]any
		if err := json.Unmarshal(event, &ev); err != nil {
			return err
		}
		// The stream now also emits a span.model_request_end at the
		// end — skip it for the content assertions.
		if ev["type"] != "agent.message" {
			return nil
		}
		content, _ := ev["content"].([]any)
		if len(content) > 0 {
			block, _ := content[0].(map[string]any)
			if text, _ := block["text"].(string); text != "" {
				received = append(received, text)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("RunTurnStream: %v", err)
	}
	if len(received) == 0 {
		t.Fatal("no events received from stream")
	}
	// Last event should have the full accumulated text.
	last := received[len(received)-1]
	if !strings.Contains(last, "one") || !strings.Contains(last, "three") {
		t.Errorf("final stream text=%q want to contain 'one' and 'three'", last)
	}

	got := stub.lastRequest()
	if !got.Stream {
		t.Error("expected stream=true in request")
	}
	if got.SessionKey != "oma-sess-stream" {
		t.Errorf("sessionKey=%q want oma-sess-stream", got.SessionKey)
	}
}

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

func TestBuildMessages(t *testing.T) {
	msgs := buildMessages("You are helpful.", "hello")
	if len(msgs) != 2 {
		t.Fatalf("len=%d want 2", len(msgs))
	}
	if msgs[0].Role != "system" || msgs[0].Content != "You are helpful." {
		t.Errorf("system=%+v", msgs[0])
	}
	if msgs[1].Role != "user" || msgs[1].Content != "hello" {
		t.Errorf("user=%+v", msgs[1])
	}

	// Without system prompt.
	msgs2 := buildMessages("", "hello")
	if len(msgs2) != 1 {
		t.Fatalf("len=%d want 1 (no system)", len(msgs2))
	}
	if msgs2[0].Role != "user" {
		t.Errorf("first should be user, got %s", msgs2[0].Role)
	}
}
