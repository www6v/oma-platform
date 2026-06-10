package harness_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/open-ma/oma-building/internal/harness"
)

func TestRunTurnStreamingFallsBackToRunTurn(t *testing.T) {
	var seen int
	err := harness.RunTurnStreaming(
		context.Background(),
		&harness.FakeClient{Text: "batch"},
		harness.TurnRequest{},
		func(ev json.RawMessage) error {
			seen++
			var meta struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(ev, &meta)
			if meta.Type != "agent.message" {
				t.Fatalf("type=%q", meta.Type)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if seen != 1 {
		t.Fatalf("events=%d want 1", seen)
	}
}

func TestHTTPClientRunTurnStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/turn/stream" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(
			`{"type":"agent.tool_use","id":"t1","name":"x","input":{}}` + "\n",
		))
		_, _ = w.Write([]byte(
			`{"type":"agent.message","content":[{"type":"text","text":"hi"}]}` + "\n",
		))
	}))
	defer server.Close()

	client := &harness.HTTPClient{BaseURL: server.URL}
	var types []string
	err := client.RunTurnStream(
		context.Background(),
		harness.TurnRequest{SessionID: "s1"},
		func(ev json.RawMessage) error {
			var meta struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(ev, &meta)
			types = append(types, meta.Type)
			return nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	want := "agent.tool_use,agent.message"
	got := strings.Join(types, ",")
	if got != want {
		t.Fatalf("types=%q want %q", got, want)
	}
}
