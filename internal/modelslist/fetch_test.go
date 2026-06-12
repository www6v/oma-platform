package modelslist_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/open-ma/oma-building/internal/modelslist"
)

func TestFetchAnthropicModels(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if r.Header.Get("x-api-key") != "ant-key" {
			t.Fatalf("missing x-api-key")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"data": [
				{"id": "claude-sonnet-4-6", "display_name": "Claude Sonnet 4.6"},
				{"id": "claude-haiku", "display_name": ""}
			]
		}`))
	}))
	t.Cleanup(srv.Close)

	client := &modelslist.Client{
		HTTP:             srv.Client(),
		AnthropicBaseURL: srv.URL,
	}
	got, err := client.Fetch(context.Background(), "ant", "ant-key")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("len=%d", len(got))
	}
	if got[0].ID != "claude-sonnet-4-6" || got[0].Name != "Claude Sonnet 4.6" {
		t.Fatalf("first=%+v", got[0])
	}
	if got[1].Name != "claude-haiku" {
		t.Fatalf("fallback name=%q", got[1].Name)
	}
}

func TestFetchOpenAIFiltersChatModels(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer oai-key" {
			t.Fatalf("missing bearer")
		}
		_, _ = w.Write([]byte(`{
			"data": [
				{"id": "whisper-1"},
				{"id": "gpt-4o"},
				{"id": "o3-mini"}
			]
		}`))
	}))
	t.Cleanup(srv.Close)

	client := &modelslist.Client{
		HTTP:          srv.Client(),
		OpenAIBaseURL: srv.URL,
	}
	got, err := client.Fetch(context.Background(), "oai", "oai-key")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got=%+v", got)
	}
	if got[0].ID != "gpt-4o" || got[1].ID != "o3-mini" {
		t.Fatalf("order=%+v", got)
	}
}

func TestFetchUnknownProviderReturnsEmpty(t *testing.T) {
	t.Parallel()

	client := &modelslist.Client{}
	got, err := client.Fetch(context.Background(), "unknown", "key")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got=%+v", got)
	}
}

func TestFetchAnthropicUpstreamError(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)

	client := &modelslist.Client{
		HTTP:             srv.Client(),
		AnthropicBaseURL: srv.URL,
	}
	_, err := client.Fetch(context.Background(), "ant", "bad-key")
	if err == nil {
		t.Fatal("expected error")
	}
}
