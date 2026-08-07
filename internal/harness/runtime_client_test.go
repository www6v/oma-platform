package harness

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestRuntimeClientRunTurnStream(t *testing.T) {
	var gotStart map[string]any
	var gotPrompt map[string]any
	upgrader := websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path !=
				"/v1/internal/runtimes/rt-local/attach-harness" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if got := r.URL.Query().Get("replay"); got != "0" {
				t.Errorf("replay=%q, want 0", got)
			}
			if got := r.Header.Get("x-internal-secret"); got != "secret" {
				t.Errorf("internal secret=%q", got)
			}
			if got := r.Header.Get("x-session-id"); got != "sess-local" {
				t.Errorf("session id=%q", got)
			}
			if got := r.Header.Get("x-harness-tenant"); got != "tenant-local" {
				t.Errorf("tenant id=%q", got)
			}

			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Errorf("upgrade: %v", err)
				return
			}
			defer conn.Close()

			if err := conn.WriteJSON(map[string]any{
				"type":          "attached",
				"daemon_online": true,
			}); err != nil {
				t.Errorf("write attached: %v", err)
				return
			}
			if err := conn.ReadJSON(&gotStart); err != nil {
				t.Errorf("read start: %v", err)
				return
			}
			if err := conn.WriteJSON(map[string]any{
				"type":           "session.ready",
				"session_id":     "sess-local",
				"acp_session_id": "acp-local",
			}); err != nil {
				t.Errorf("write ready: %v", err)
				return
			}
			if err := conn.ReadJSON(&gotPrompt); err != nil {
				t.Errorf("read prompt: %v", err)
				return
			}
			if err := conn.WriteJSON(map[string]any{
				"type":       "session.event",
				"session_id": "sess-local",
				"turn_id":    gotPrompt["turn_id"],
				"event": map[string]any{
					"sessionId": "acp-local",
					"update": map[string]any{
						"sessionUpdate": "agent_message_chunk",
						"messageId":     "msg-replayed",
						"content": map[string]any{
							"type": "text",
							"text": "OLD",
						},
					},
				},
			}); err != nil {
				t.Errorf("write replayed event: %v", err)
				return
			}
			if err := conn.WriteJSON(map[string]any{
				"type":       "session.event",
				"session_id": "sess-local",
				"turn_id":    gotPrompt["turn_id"],
				"event": map[string]any{
					"sessionId": "acp-local",
					"update": map[string]any{
						"sessionUpdate": "agent_message_chunk",
						"messageId":     "msg-local",
						"content": map[string]any{
							"type": "text",
							"text": "PONG",
						},
					},
				},
			}); err != nil {
				t.Errorf("write event: %v", err)
				return
			}
			if err := conn.WriteJSON(map[string]any{
				"type":       "session.complete",
				"session_id": "sess-local",
				"turn_id":    gotPrompt["turn_id"],
			}); err != nil {
				t.Errorf("write complete: %v", err)
			}
		},
	))
	defer server.Close()

	client := &RuntimeClient{
		PlatformBase:   server.URL,
		InternalSecret: "secret",
		Binding: AcpBinding{
			RuntimeID:  "rt-local",
			AcpAgentID: "codex-cli",
		},
	}
	req := TurnRequest{
		SessionID: "sess-local",
		TenantID:  "tenant-local",
		Events: []json.RawMessage{
			json.RawMessage(
				`{"type":"user.message","content":[{"type":"text","text":"ping"}]}`,
			),
		},
	}
	var events []json.RawMessage
	err := client.RunTurnStream(
		context.Background(),
		req,
		func(event json.RawMessage) error {
			events = append(events, append(json.RawMessage(nil), event...))
			return nil
		},
	)
	if err != nil {
		t.Fatalf("RunTurnStream: %v", err)
	}

	if gotStart["type"] != "session.start" ||
		gotStart["agent_id"] != "codex-cli" {
		t.Fatalf("unexpected session.start: %+v", gotStart)
	}
	if gotPrompt["type"] != "session.prompt" ||
		gotPrompt["text"] != "ping" {
		t.Fatalf("unexpected session.prompt: %+v", gotPrompt)
	}

	var final struct {
		Type    string `json:"type"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	for _, event := range events {
		var meta struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(event, &meta) == nil && meta.Type == "agent.message" {
			if err := json.Unmarshal(event, &final); err != nil {
				t.Fatal(err)
			}
		}
	}
	if final.Type != "agent.message" || len(final.Content) != 1 ||
		final.Content[0].Text != "PONG" {
		t.Fatalf("missing final agent.message in %s", joinRaw(events))
	}
}

func TestRuntimeClientRejectsOfflineDaemon(t *testing.T) {
	upgrader := websocket.Upgrader{
		CheckOrigin: func(*http.Request) bool { return true },
	}
	server := httptest.NewServer(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			conn, err := upgrader.Upgrade(w, r, nil)
			if err != nil {
				t.Errorf("upgrade: %v", err)
				return
			}
			defer conn.Close()
			_ = conn.WriteJSON(map[string]any{
				"type":          "attached",
				"daemon_online": false,
			})
		},
	))
	defer server.Close()

	client := &RuntimeClient{
		PlatformBase:   server.URL,
		InternalSecret: "secret",
		Binding: AcpBinding{
			RuntimeID:  "rt-local",
			AcpAgentID: "codex-cli",
		},
	}
	_, err := client.RunTurn(context.Background(), TurnRequest{
		SessionID: "sess-local",
		TenantID:  "tenant-local",
	})
	if err == nil || !strings.Contains(err.Error(), "daemon offline") {
		t.Fatalf("err=%v, want daemon offline", err)
	}
}

func joinRaw(events []json.RawMessage) string {
	parts := make([]string, 0, len(events))
	for _, event := range events {
		parts = append(parts, string(event))
	}
	return strings.Join(parts, "\n")
}
