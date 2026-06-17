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

func testTeamRouterWithAPIKeys(
	t *testing.T,
	db *sql.DB,
) (
	http.Handler,
	*store.ApiKeyRepo,
	*store.AgentRepo,
	*store.SessionRepo,
	*store.TeamRepo,
) {
	t.Helper()
	agents := store.NewAgentRepo(db)
	environments := store.NewEnvironmentRepo(db)
	if err := environments.EnsureDefault(context.Background()); err != nil {
		t.Fatal(err)
	}
	modelCards := store.NewModelCardRepo(db)
	apiKeys := store.NewApiKeyRepo(db)
	sessions := store.NewSessionRepo(db, agents, environments)
	events := store.NewEventRepo(db)
	pending := store.NewPendingRepo(db)
	hub := stream.NewHub()
	reg := session.NewRegistry()
	workdirs := workdir.NewManager(t.TempDir(), "")
	outputs := sessionoutputs.NewStore(t.TempDir())
	models := &modelresolve.Resolver{Cards: modelCards}
	teams := store.NewTeamRepo(db)
	fileBlobs := fileblob.NewStore(t.TempDir())
	files := store.NewFileRepo(db)

	handler := api.NewRouter(api.Deps{
		Agents:       agents,
		Environments: environments,
		ModelCards:   modelCards,
		ApiKeys:      apiKeys,
		AuthDisabled: false,
		SessionOutputs: outputs,
		Sessions: api.NewSessionHandlers(
			sessions, agents, events, pending, hub, reg, workdirs,
			outputs, &harness.FakeClient{}, models,
			&harness.ResourceResolver{Files: files, FileBlobs: fileBlobs},
			store.NewWakeupRepo(db),
			teams,
			"", "", "", "", "", "", "",
		),
	})
	return handler, apiKeys, agents, sessions, teams
}

func ensureEnvForTenant(
	t *testing.T,
	envs *store.EnvironmentRepo,
	tenantID string,
) string {
	t.Helper()
	ctx := context.Background()
	cfg, err := json.Marshal(map[string]string{"type": "local"})
	if err != nil {
		t.Fatal(err)
	}
	env, err := envs.Create(ctx, store.CreateEnvironmentInput{
		TenantID: tenantID,
		Name:     "local-default",
		Config:   cfg,
	})
	if err != nil {
		t.Fatal(err)
	}
	return env.ID
}

func TestSessionTeamRoutesTenantIsolation(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	handler, apiKeys, agents, sessions, teams := testTeamRouterWithAPIKeys(t, db)
	envs := store.NewEnvironmentRepo(db)
	envA := ensureEnvForTenant(t, envs, "tenant-a")
	envB := ensureEnvForTenant(t, envs, "tenant-b")
	ctx := context.Background()
	now := time.Now().UnixMilli()

	keyA, err := apiKeys.Mint(ctx, "tenant-a", "user-a", "key-a", "test")
	if err != nil {
		t.Fatal(err)
	}
	keyB, err := apiKeys.Mint(ctx, "tenant-b", "user-b", "key-b", "test")
	if err != nil {
		t.Fatal(err)
	}

	agentA, err := agents.Create(ctx, store.CreateAgentInput{
		TenantID: "tenant-a",
		Name:     "lead-a",
		Model:    "claude-sonnet-4-20250514",
	})
	if err != nil {
		t.Fatal(err)
	}
	agentB, err := agents.Create(ctx, store.CreateAgentInput{
		TenantID: "tenant-b",
		Name:     "lead-b",
		Model:    "claude-sonnet-4-20250514",
	})
	if err != nil {
		t.Fatal(err)
	}

	sessA, err := sessions.Create(ctx, store.CreateSessionInput{
		TenantID:      "tenant-a",
		AgentID:       agentA.ID,
		EnvironmentID: envA,
	})
	if err != nil {
		t.Fatal(err)
	}
	sessB, err := sessions.Create(ctx, store.CreateSessionInput{
		TenantID:      "tenant-b",
		AgentID:       agentB.ID,
		EnvironmentID: envB,
	})
	if err != nil {
		t.Fatal(err)
	}

	teamA := store.Team{
		ID:           store.NewTeamID(),
		SessionID:    sessA.ID,
		TenantID:     "tenant-a",
		Name:         "alpha",
		LeadThreadID: "sthr_primary",
		LeadAgentID:  agentA.ID,
		Status:       "active",
		CreatedAt:    now,
	}
	teamB := store.Team{
		ID:           store.NewTeamID(),
		SessionID:    sessB.ID,
		TenantID:     "tenant-b",
		Name:         "beta",
		LeadThreadID: "sthr_primary",
		LeadAgentID:  agentB.ID,
		Status:       "active",
		CreatedAt:    now,
	}
	if err := teams.CreateTeam(ctx, teamA); err != nil {
		t.Fatal(err)
	}
	if err := teams.CreateTeam(ctx, teamB); err != nil {
		t.Fatal(err)
	}

	workerA := store.TeamMember{
		ID:          store.NewTeamMemberID(),
		TeamID:      teamA.ID,
		AgentID:     agentA.ID,
		DisplayName: "coder",
		ThreadID:    store.NewThreadID(),
		BackendType: "in_process",
		Status:      "listening",
		JoinedAt:    now,
	}
	workerB := store.TeamMember{
		ID:          store.NewTeamMemberID(),
		TeamID:      teamB.ID,
		AgentID:     agentB.ID,
		DisplayName: "coder",
		ThreadID:    store.NewThreadID(),
		BackendType: "in_process",
		Status:      "listening",
		JoinedAt:    now,
	}
	if err := teams.CreateMember(ctx, workerA); err != nil {
		t.Fatal(err)
	}
	if err := teams.CreateMember(ctx, workerB); err != nil {
		t.Fatal(err)
	}

	assertTeamListOK := func(t *testing.T, key, sessionID string) {
		t.Helper()
		url := "/v1/sessions/" + sessionID + "/teams"
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("x-api-key", key)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("list teams status=%d body=%s", rec.Code, rec.Body.String())
		}
	}

	assertTeamListNotFound := func(t *testing.T, key, sessionID string) {
		t.Helper()
		url := "/v1/sessions/" + sessionID + "/teams"
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("x-api-key", key)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got status=%d body=%s", rec.Code, rec.Body.String())
		}
	}

	assertMessagesNotFound := func(t *testing.T, key, sessionID, teamID string) {
		t.Helper()
		url := "/v1/sessions/" + sessionID + "/teams/" + teamID + "/messages"
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("x-api-key", key)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got status=%d body=%s", rec.Code, rec.Body.String())
		}
	}

	assertShutdownNotFound := func(
		t *testing.T,
		key, sessionID, teamID, memberID string,
	) {
		t.Helper()
		url := "/v1/sessions/" + sessionID +
			"/teams/" + teamID + "/members/" + memberID + "/shutdown"
		req := httptest.NewRequest(http.MethodPost, url, nil)
		req.Header.Set("x-api-key", key)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got status=%d body=%s", rec.Code, rec.Body.String())
		}
	}

	// Same tenant: allowed.
	assertTeamListOK(t, keyA.Key, sessA.ID)
	assertTeamListOK(t, keyB.Key, sessB.ID)

	// Cross-tenant: session not visible → 404.
	assertTeamListNotFound(t, keyA.Key, sessB.ID)
	assertTeamListNotFound(t, keyB.Key, sessA.ID)

	// Cross-tenant: messages/shutdown on foreign session+team → 404.
	assertMessagesNotFound(t, keyA.Key, sessB.ID, teamB.ID)
	assertMessagesNotFound(t, keyB.Key, sessA.ID, teamA.ID)
	assertShutdownNotFound(t, keyA.Key, sessB.ID, teamB.ID, workerB.ID)
	assertShutdownNotFound(t, keyB.Key, sessA.ID, teamA.ID, workerA.ID)

	// Same tenant but wrong session for team id → 404.
	assertMessagesNotFound(t, keyA.Key, sessA.ID, teamB.ID)
	assertShutdownNotFound(t, keyA.Key, sessA.ID, teamB.ID, workerB.ID)
}
