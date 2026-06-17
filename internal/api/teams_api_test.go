package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/store"
)

func TestSessionTeamMessagesAndShutdown(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	handler, _, sessions := testRouterSharedDB(t, db, nil)
	ctx := context.Background()
	agents := store.NewAgentRepo(db)
	environments := store.NewEnvironmentRepo(db)
	if err := environments.EnsureDefault(ctx); err != nil {
		t.Fatal(err)
	}
	agent, err := agents.Create(ctx, store.CreateAgentInput{
		TenantID: "default",
		Name:     "lead",
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

	teams := store.NewTeamRepo(db)
	now := time.Now().UnixMilli()
	team := store.Team{
		ID:           store.NewTeamID(),
		SessionID:    sess.ID,
		TenantID:     "default",
		Name:         "alpha",
		LeadThreadID: "sthr_primary",
		LeadAgentID:  agent.ID,
		Status:       "active",
		CreatedAt:    now,
	}
	if err := teams.CreateTeam(ctx, team); err != nil {
		t.Fatal(err)
	}
	leadMember := store.TeamMember{
		ID:          store.NewTeamMemberID(),
		TeamID:      team.ID,
		AgentID:     agent.ID,
		DisplayName: "lead",
		ThreadID:    "sthr_primary",
		BackendType: "in_process",
		Status:      "idle",
		JoinedAt:    now,
	}
	worker := store.TeamMember{
		ID:          store.NewTeamMemberID(),
		TeamID:      team.ID,
		AgentID:     "agt-worker",
		DisplayName: "coder",
		ThreadID:    store.NewThreadID(),
		BackendType: "in_process",
		Status:      "listening",
		JoinedAt:    now,
	}
	if err := teams.CreateMember(ctx, leadMember); err != nil {
		t.Fatal(err)
	}
	if err := teams.CreateMember(ctx, worker); err != nil {
		t.Fatal(err)
	}

	msgURL := "/v1/sessions/" + sess.ID + "/teams/" + team.ID + "/messages"
	req := httptest.NewRequest(http.MethodGet, msgURL, nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("messages status=%d body=%s", rec.Code, rec.Body.String())
	}

	shutdownURL := "/v1/sessions/" + sess.ID +
		"/teams/" + team.ID + "/members/" + worker.ID + "/shutdown"
	req = httptest.NewRequest(http.MethodPost, shutdownURL, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("shutdown status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, msgURL, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("messages after shutdown status=%d", rec.Code)
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	data, ok := resp["data"].([]any)
	if !ok || len(data) != 1 {
		t.Fatalf("expected 1 message, got %v", resp["data"])
	}
	item := data[0].(map[string]any)
	if item["message_type"] != "shutdown_request" {
		t.Fatalf("message_type=%v", item["message_type"])
	}
}
