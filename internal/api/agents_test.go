package api_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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

func testRouterDeps(
	t *testing.T,
	db *sql.DB,
	client harness.Client,
	outputsDir string,
) (api.Deps, *session.Registry) {
	t.Helper()
	agents := store.NewAgentRepo(db)
	environments := store.NewEnvironmentRepo(db)
	if err := environments.EnsureDefault(context.Background()); err != nil {
		t.Fatal(err)
	}
	modelCards := store.NewModelCardRepo(db)
	vaults := store.NewVaultRepo(db)
	credentials := store.NewCredentialRepo(db)
	skillFiles := store.NewSkillFileStore(t.TempDir())
	fileBlobs := fileblob.NewStore(t.TempDir())
	files := store.NewFileRepo(db)
	skills := store.NewSkillRepo(db, skillFiles)
	models := &modelresolve.Resolver{Cards: modelCards}
	sessions := store.NewSessionRepo(db, agents, environments)
	events := store.NewEventRepo(db)
	pending := store.NewPendingRepo(db)
	hub := stream.NewHub()
	reg := session.NewRegistry()
	workdirs := workdir.NewManager(t.TempDir(), "")
	if outputsDir == "" {
		outputsDir = t.TempDir()
	}
	outputs := sessionoutputs.NewStore(outputsDir)

	integrations := store.NewIntegrationRepo(db)
	sessionHandlers := api.NewSessionHandlers(
		sessions, events, pending, hub, reg, workdirs,
		outputs, client, models, "", "", "", "",
	)
	const testInternalSecret = "test-internal-secret"
	deps := api.Deps{
		Agents:         agents,
		Environments:   environments,
		ModelCards:     modelCards,
		Vaults:         vaults,
		Credentials:    credentials,
		Skills:         skills,
		SkillFiles:     skillFiles,
		Files:          files,
		FileBlobs:      fileBlobs,
		SessionOutputs: outputs,
		ApiKeys:        store.NewApiKeyRepo(db),
		Tenants:        store.NewTenantRepo(db),
		Runtimes:       store.NewRuntimeRepo(db),
		Integrations:   integrations,
		MemoryStores:   store.NewMemoryStoreRepo(db),
		EvalRuns:       store.NewEvalRunRepo(db),
		AuthDisabled:   true,
		Sessions:       sessionHandlers,
		InternalSecret: testInternalSecret,
		LinearGateway: api.NewLinearGatewayHandler(
			integrations, sessionHandlers, "http://test", testInternalSecret,
		),
	}
	return deps, reg
}

func testRouter(t *testing.T) http.Handler {
	t.Helper()
	handler, _ := testRouterHarness(t, &harness.FakeClient{})
	return handler
}

func testRouterWithOutputs(t *testing.T, outputsDir string) http.Handler {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	deps, _ := testRouterDeps(t, db, &harness.FakeClient{}, outputsDir)
	return api.NewRouter(deps)
}

func testRouterHarness(
	t *testing.T,
	client harness.Client,
) (http.Handler, *session.Registry) {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	deps, reg := testRouterDeps(t, db, client, "")
	return api.NewRouter(deps), reg
}

func testRouterSharedDB(
	t *testing.T,
	db *sql.DB,
	client harness.Client,
) (http.Handler, *session.Registry, *store.SessionRepo) {
	t.Helper()
	agents := store.NewAgentRepo(db)
	environments := store.NewEnvironmentRepo(db)
	if err := environments.EnsureDefault(context.Background()); err != nil {
		t.Fatal(err)
	}
	modelCards := store.NewModelCardRepo(db)
	vaults := store.NewVaultRepo(db)
	credentials := store.NewCredentialRepo(db)
	skillFiles := store.NewSkillFileStore(t.TempDir())
	fileBlobs := fileblob.NewStore(t.TempDir())
	files := store.NewFileRepo(db)
	skills := store.NewSkillRepo(db, skillFiles)
	models := &modelresolve.Resolver{Cards: modelCards}
	sessions := store.NewSessionRepo(db, agents, environments)
	events := store.NewEventRepo(db)
	pending := store.NewPendingRepo(db)
	hub := stream.NewHub()
	reg := session.NewRegistry()
	workdirs := workdir.NewManager(t.TempDir(), "")
	outputs := sessionoutputs.NewStore(t.TempDir())

	handler := api.NewRouter(api.Deps{
		Agents:         agents,
		Environments:   environments,
		ModelCards:     modelCards,
		Vaults:         vaults,
		Credentials:    credentials,
		Skills:         skills,
		SkillFiles:     skillFiles,
		Files:          files,
		FileBlobs:      fileBlobs,
		SessionOutputs: outputs,
		ApiKeys:        store.NewApiKeyRepo(db),
		Tenants:        store.NewTenantRepo(db),
		Runtimes:       store.NewRuntimeRepo(db),
		Integrations:   store.NewIntegrationRepo(db),
		MemoryStores:   store.NewMemoryStoreRepo(db),
		EvalRuns:       store.NewEvalRunRepo(db),
		AuthDisabled:   true,
		Sessions: api.NewSessionHandlers(
			sessions, events, pending, hub, reg, workdirs, outputs, client, models, "", "", "", "",
		),
	})
	return handler, reg, sessions
}

func TestPostAgent(t *testing.T) {
	handler := testRouter(t)
	body := `{"name":"demo","model":"claude-sonnet-4-20250514","system_prompt":"hi"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["version"].(float64) != 1 {
		t.Fatalf("version=%v", resp["version"])
	}
}

func TestPostAgentRejectsStringTools(t *testing.T) {
	handler := testRouter(t)
	body := `{"name":"demo","model":"claude-sonnet-4-20250514","tools":"agent_toolset_20260401"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostAgentWithTools(t *testing.T) {
	handler := testRouter(t)
	body := `{"name":"demo","model":"claude-sonnet-4-20250514","description":"hi","tools":[{"type":"agent_toolset_20260401"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["description"] != "hi" {
		t.Fatalf("description=%v", resp["description"])
	}
	tools, ok := resp["tools"].([]any)
	if !ok || len(tools) != 1 {
		t.Fatalf("tools=%v", resp["tools"])
	}
}

func TestAgentVersionsAPI(t *testing.T) {
	handler := testRouter(t)

	createBody := `{"name":"v1","model":"claude-sonnet-4-20250514"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewBufferString(createBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d", rec.Code)
	}
	var agent map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &agent)
	id := agent["id"].(string)

	patchBody := `{"name":"v2"}`
	req = httptest.NewRequest(http.MethodPatch, "/v1/agents/"+id, bytes.NewBufferString(patchBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/agents/"+id+"/versions", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list versions status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listResp map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &listResp)
	data := listResp["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("versions=%v", data)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/agents/"+id+"/versions/1", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get version status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/agents/"+id+"/versions/2", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("current version should 404, status=%d", rec.Code)
	}
}

func TestPostSessionInterruptEventAccepted(t *testing.T) {
	handler := testRouter(t)

	agentBody := `{"name":"s","model":"claude-sonnet-4-20250514"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewBufferString(agentBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var agent map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &agent)

	sessBody := `{"agent":"` + agent["id"].(string) + `"}`
	req = httptest.NewRequest(http.MethodPost, "/v1/sessions", bytes.NewBufferString(sessBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var sess map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &sess)

	evBody := `{"events":[{"type":"user.interrupt"}]}`
	path := "/v1/sessions/" + sess["id"].(string) + "/events"
	req = httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(evBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("events status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestPostEnvironmentAndModelCard(t *testing.T) {
	handler := testRouter(t)

	envBody := `{"name":"dev","config":{"type":"local"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/environments", bytes.NewBufferString(envBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("env status=%d body=%s", rec.Code, rec.Body.String())
	}

	cardBody := `{"model_id":"claude-prod","provider":"ant","api_key":"sk-test-9999","is_default":true}`
	req = httptest.NewRequest(http.MethodPost, "/v1/model_cards", bytes.NewBufferString(cardBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("card status=%d body=%s", rec.Code, rec.Body.String())
	}
	var card map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &card)
	if card["api_key_preview"] != "9999" {
		t.Fatalf("preview=%v", card["api_key_preview"])
	}
}

func TestPostSessionAndEvents(t *testing.T) {
	handler := testRouter(t)

	agentBody := `{"name":"s","model":"claude-sonnet-4-20250514"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/agents", bytes.NewBufferString(agentBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("agent status=%d", rec.Code)
	}
	var agent map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &agent)

	sessBody := `{"agent":"` + agent["id"].(string) + `"}`
	req = httptest.NewRequest(http.MethodPost, "/v1/sessions", bytes.NewBufferString(sessBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("session status=%d body=%s", rec.Code, rec.Body.String())
	}
	var sess map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &sess)

	evBody := `{"events":[{"type":"user.message","content":[{"type":"text","text":"hi"}]}]}`
	path := "/v1/sessions/" + sess["id"].(string) + "/events"
	req = httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(evBody))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("events status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAgentsListPagination(t *testing.T) {
	handler := testRouter(t)
	names := []string{"alpha", "beta", "gamma"}
	for _, name := range names {
		body := `{"name":"` + name + `","model":"claude-sonnet-4-20250514"}`
		req := httptest.NewRequest(
			http.MethodPost,
			"/v1/agents",
			bytes.NewBufferString(body),
		)
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/agents?limit=2", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("page1 status=%d body=%s", rec.Code, rec.Body.String())
	}
	var page1 struct {
		Data       []map[string]any `json:"data"`
		NextCursor string           `json:"next_cursor"`
		HasMore    bool             `json:"has_more"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page1); err != nil {
		t.Fatal(err)
	}
	if len(page1.Data) != 2 {
		t.Fatalf("page1 len=%d", len(page1.Data))
	}
	if page1.NextCursor == "" || !page1.HasMore {
		t.Fatalf("page1 cursor=%q has_more=%v", page1.NextCursor, page1.HasMore)
	}

	req = httptest.NewRequest(
		http.MethodGet,
		"/v1/agents?limit=2&cursor="+page1.NextCursor,
		nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("page2 status=%d body=%s", rec.Code, rec.Body.String())
	}
	var page2 struct {
		Data       []map[string]any `json:"data"`
		NextCursor string           `json:"next_cursor"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page2); err != nil {
		t.Fatal(err)
	}
	if len(page2.Data) != 1 {
		t.Fatalf("page2 len=%d", len(page2.Data))
	}
	if page2.NextCursor != "" {
		t.Fatalf("expected no next cursor, got %q", page2.NextCursor)
	}
}
