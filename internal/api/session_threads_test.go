package api

import (
	"encoding/json"
	"testing"

	"github.com/open-ma/oma-building/internal/store"
)

func TestDeriveSessionThreadsPrimaryOnly(t *testing.T) {
	t.Parallel()
	now := int64(1_700_000_000_000)
	sess := &store.Session{
		ID:        "sess_test",
		AgentID:   "agt_main",
		CreatedAt: now,
		AgentSnapshot: json.RawMessage(
			`{"id":"agt_main","name":"MainAgent","model":"claude-sonnet"}`,
		),
	}

	threads := deriveSessionThreads(sess, nil, false)
	if len(threads) != 1 {
		t.Fatalf("len=%d want 1", len(threads))
	}
	primary := threads[0]
	if primary["id"] != primaryThreadID {
		t.Fatalf("id=%v", primary["id"])
	}
	if primary["parent_thread_id"] != nil {
		t.Fatalf("parent=%v want nil", primary["parent_thread_id"])
	}
	if primary["agent_id"] != "agt_main" {
		t.Fatalf("agent_id=%v", primary["agent_id"])
	}
	if primary["agent_name"] != "MainAgent" {
		t.Fatalf("agent_name=%v", primary["agent_name"])
	}
	if primary["status"] != "active" {
		t.Fatalf("status=%v", primary["status"])
	}
}

func TestDeriveSessionThreadsFromEvents(t *testing.T) {
	t.Parallel()
	now := int64(1_700_000_000_000)
	sess := &store.Session{
		ID:        "sess_test",
		AgentID:   "agt_main",
		CreatedAt: now,
		AgentSnapshot: json.RawMessage(
			`{"id":"agt_main","name":"MainAgent"}`,
		),
	}
	events := []store.StoredEvent{
		{
			Type: "session.thread_created",
			Payload: json.RawMessage(`{
				"type": "session.thread_created",
				"session_thread_id": "sthr_subA",
				"agent_id": "agt_worker",
				"agent_name": "WorkerA",
				"parent_thread_id": "sthr_primary"
			}`),
			CreatedAt: now + 1000,
		},
		{
			Type: "session.thread_created",
			Payload: json.RawMessage(`{
				"type": "session.thread_created",
				"session_thread_id": "sthr_subB",
				"agent_id": "agt_worker",
				"agent_name": "WorkerB",
				"parent_thread_id": "sthr_primary"
			}`),
			CreatedAt: now + 2000,
		},
		{
			Type: "session.thread_created",
			Payload: json.RawMessage(`{
				"type": "session.thread_created",
				"session_thread_id": "sthr_subA",
				"agent_id": "agt_worker",
				"agent_name": "Dup"
			}`),
			CreatedAt: now + 3000,
		},
	}

	threads := deriveSessionThreads(sess, events, false)
	if len(threads) != 3 {
		t.Fatalf("len=%d want 3", len(threads))
	}
	if threads[0]["id"] != primaryThreadID {
		t.Fatalf("first id=%v", threads[0]["id"])
	}
	if threads[1]["id"] != "sthr_subA" {
		t.Fatalf("second id=%v", threads[1]["id"])
	}
	if threads[1]["agent_name"] != "WorkerA" {
		t.Fatalf("subA name=%v", threads[1]["agent_name"])
	}
	if threads[2]["id"] != "sthr_subB" {
		t.Fatalf("third id=%v", threads[2]["id"])
	}
}
