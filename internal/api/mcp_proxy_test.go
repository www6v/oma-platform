package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/mcpproxy"
	"github.com/open-ma/oma-building/internal/store"
)

type stubSessionStore struct {
	sess *store.Session
}

func (s stubSessionStore) Get(
	_ context.Context,
	_, _ string,
) (*store.Session, error) {
	if s.sess == nil {
		return nil, store.ErrNotFound
	}
	return s.sess, nil
}

func TestMcpProxyForwardsRequestBody(t *testing.T) {
	t.Parallel()

	var gotBody []byte
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		gotBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{}}`))
	}))
	t.Cleanup(upstream.Close)

	snapshot, _ := json.Marshal(map[string]any{
		"mcp_servers": []map[string]string{
			{
				"name":                "smoke",
				"url":                 upstream.URL,
				"authorization_token": "tok",
			},
		},
	})

	r := chi.NewRouter()
	mountMcpProxyRoutes(r, mcpProxyDeps{
		Resolver: &mcpproxy.Resolver{
			Sessions: stubSessionStore{
				sess: &store.Session{AgentSnapshot: snapshot},
			},
		},
		APIKey: "dev-key",
	})

	payload := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/mcp-proxy/sess-1/smoke",
		bytes.NewReader(payload),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "dev-key")

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if string(gotBody) != string(payload) {
		t.Fatalf("upstream body=%q want=%q", gotBody, payload)
	}
}
