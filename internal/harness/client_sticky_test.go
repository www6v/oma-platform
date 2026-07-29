package harness_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/open-ma/oma-building/internal/harness"
)

func TestHTTPClientRunTurnSetsStickySessionHeader(t *testing.T) {
	var gotHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/turn" {
			http.NotFound(w, r)
			return
		}
		gotHeader = r.Header.Get(harness.StickySessionHeader)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"events":[]}`)
	}))
	defer server.Close()

	client := &harness.HTTPClient{BaseURL: server.URL}
	_, err := client.RunTurn(context.Background(), harness.TurnRequest{
		SessionID: "sess-sticky-1",
		Events:    []json.RawMessage{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotHeader != "sess-sticky-1" {
		t.Fatalf("sticky header=%q want sess-sticky-1", gotHeader)
	}
}

func TestHTTPClientRunTurnOmitsStickyHeaderWhenEmpty(t *testing.T) {
	var present bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, present = r.Header[harness.StickySessionHeader]
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"events":[]}`)
	}))
	defer server.Close()

	client := &harness.HTTPClient{BaseURL: server.URL}
	_, err := client.RunTurn(context.Background(), harness.TurnRequest{
		Events: []json.RawMessage{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if present {
		t.Fatal("expected sticky header omitted when session_id empty")
	}
}

func TestHTTPClientRunTurnStreamSetsStickySessionHeader(t *testing.T) {
	var gotHeader string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get(harness.StickySessionHeader)
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(
			`{"type":"agent.message","content":[{"type":"text","text":"ok"}]}` + "\n",
		))
	}))
	defer server.Close()

	client := &harness.HTTPClient{BaseURL: server.URL}
	err := client.RunTurnStream(
		context.Background(),
		harness.TurnRequest{SessionID: "sess-stream"},
		func(json.RawMessage) error { return nil },
	)
	if err != nil {
		t.Fatal(err)
	}
	if gotHeader != "sess-stream" {
		t.Fatalf("sticky header=%q want sess-stream", gotHeader)
	}
}
