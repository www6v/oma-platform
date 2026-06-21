package api_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/api"
	"github.com/open-ma/oma-building/internal/fileblob"
	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/modelresolve"
	"github.com/open-ma/oma-building/internal/session"
	"github.com/open-ma/oma-building/internal/sessionoutputs"
	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/stream"
	"github.com/open-ma/oma-building/internal/workdir"
)

// testTaskRouter builds a minimal auth-disabled router with a real TeamTaskRepo.
func testTaskRouter(t *testing.T, db *sql.DB) (
	http.Handler,
	*store.TeamRepo,
	*store.TeamTaskRepo,
	*store.SessionRepo,
) {
	t.Helper()
	agents := store.NewAgentRepo(db)
	environments := store.NewEnvironmentRepo(db)
	if err := environments.EnsureDefault(context.Background()); err != nil {
		t.Fatal(err)
	}
	modelCards := store.NewModelCardRepo(db)
	sessions := store.NewSessionRepo(db, agents, environments)
	events := store.NewEventRepo(db)
	pending := store.NewPendingRepo(db)
	hub := stream.NewHub()
	reg := session.NewRegistry()
	workdirs := workdir.NewManager(t.TempDir(), "")
	outputs := sessionoutputs.NewStore(t.TempDir())
	files := store.NewFileRepo(db)
	fileBlobs := fileblob.NewStore(t.TempDir())
	models := &modelresolve.Resolver{Cards: modelCards}
	teams := store.NewTeamRepo(db)
	tasks := store.NewTeamTaskRepo(db)

	handler := api.NewRouter(api.Deps{
		Agents:         agents,
		Environments:   environments,
		ModelCards:     modelCards,
		ApiKeys:        store.NewApiKeyRepo(db),
		AuthDisabled:   true,
		SessionOutputs: outputs,
		Sessions: api.NewSessionHandlers(
			sessions, agents, events, pending, hub, reg, workdirs,
			outputs, &harness.FakeClient{}, models,
			&harness.ResourceResolver{Files: files, FileBlobs: fileBlobs},
			store.NewWakeupRepo(db),
			teams,
			tasks,
			"", "", "", "", "", "", "",
		),
	})
	return handler, teams, tasks, sessions
}

func TestSessionTeamTasksRoute(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	handler, teams, tasks, sessions := testTaskRouter(t, db)
	ctx := context.Background()
	agents := store.NewAgentRepo(db)
	now := time.Now().UnixMilli()

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

	task1 := store.TeamTask{
		ID:        store.NewTeamTaskID(),
		TeamID:    team.ID,
		Subject:   "implement login",
		Status:    "pending",
		CreatedAt: now,
		UpdatedAt: now,
	}
	task2 := store.TeamTask{
		ID:        store.NewTeamTaskID(),
		TeamID:    team.ID,
		Subject:   "write tests",
		Status:    "in_progress",
		CreatedAt: now + 1,
		UpdatedAt: now + 1,
	}
	if err := tasks.CreateTask(ctx, task1); err != nil {
		t.Fatal(err)
	}
	if err := tasks.CreateTask(ctx, task2); err != nil {
		t.Fatal(err)
	}

	t.Run("happy path", func(t *testing.T) {
		url := "/v1/sessions/" + sess.ID + "/teams/" + team.ID + "/tasks"
		req := httptest.NewRequest(http.MethodGet, url, nil)
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
		if !ok {
			t.Fatalf("data is not array: %v", resp["data"])
		}
		if len(data) != 2 {
			t.Fatalf("expected 2 tasks, got %d", len(data))
		}
		item0 := data[0].(map[string]any)
		if item0["subject"] != "implement login" {
			t.Fatalf("task[0].subject=%v", item0["subject"])
		}
		item1 := data[1].(map[string]any)
		if item1["subject"] != "write tests" {
			t.Fatalf("task[1].subject=%v", item1["subject"])
		}
		// verify required fields are present
		for _, field := range []string{"id", "team_id", "status", "blocks", "blocked_by", "created_at", "updated_at"} {
			if _, ok := item0[field]; !ok {
				t.Errorf("task missing field %q", field)
			}
		}
	})

	t.Run("unknown session returns 404", func(t *testing.T) {
		url := "/v1/sessions/sess_nonexistent/teams/" + team.ID + "/tasks"
		req := httptest.NewRequest(http.MethodGet, url, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("unknown team returns 404", func(t *testing.T) {
		url := "/v1/sessions/" + sess.ID + "/teams/team_nonexistent/tasks"
		req := httptest.NewRequest(http.MethodGet, url, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("empty task list returns data array", func(t *testing.T) {
		emptyTeam := store.Team{
			ID:           store.NewTeamID(),
			SessionID:    sess.ID,
			TenantID:     "default",
			Name:         "empty-team",
			LeadThreadID: "sthr_primary",
			LeadAgentID:  agent.ID,
			Status:       "active",
			CreatedAt:    now,
		}
		if err := teams.CreateTeam(ctx, emptyTeam); err != nil {
			t.Fatal(err)
		}
		url := "/v1/sessions/" + sess.ID + "/teams/" + emptyTeam.ID + "/tasks"
		req := httptest.NewRequest(http.MethodGet, url, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
		}
		var resp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		data := resp["data"].([]any)
		if len(data) != 0 {
			t.Fatalf("expected empty array, got %d items", len(data))
		}
	})
}
