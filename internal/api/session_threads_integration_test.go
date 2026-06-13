package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/open-ma/oma-building/internal/store"
)

func TestSessionThreadsHTTP(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	handler, _, sessions := testRouterSharedDB(t, db, nil)
	ctx := context.Background()
	agents := store.NewAgentRepo(db)
	environments := store.NewEnvironmentRepo(db)
	_ = environments.EnsureDefault(ctx)
	agent, err := agents.Create(ctx, store.CreateAgentInput{
		TenantID: "default",
		Name:     "thread-agent",
		Model:    "claude-sonnet-4-20250514",
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.Create(ctx, store.CreateSessionInput{
		TenantID: "default",
		AgentID:  agent.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	events := store.NewEventRepo(db)
	_, err = events.AppendEvents(ctx, sess.ID, []json.RawMessage{
		json.RawMessage(`{
			"type": "session.thread_created",
			"session_thread_id": "sthr_worker",
			"agent_id": "agt_worker",
			"agent_name": "Worker",
			"parent_thread_id": "sthr_primary"
		}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(
		http.MethodGet, "/v1/sessions/"+sess.ID+"/threads", nil,
	)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	data, ok := resp["data"].([]any)
	if !ok || len(data) != 2 {
		t.Fatalf("data=%v", resp["data"])
	}
	primary := data[0].(map[string]any)
	if primary["id"] != "sthr_primary" {
		t.Fatalf("primary id=%v", primary["id"])
	}
	sub := data[1].(map[string]any)
	if sub["id"] != "sthr_worker" {
		t.Fatalf("sub id=%v", sub["id"])
	}
	if sub["agent_name"] != "Worker" {
		t.Fatalf("sub agent_name=%v", sub["agent_name"])
	}
}
