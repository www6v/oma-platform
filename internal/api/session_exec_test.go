package api_test

import (
	"bytes"
	"context"
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

func TestSessionExecLocal(t *testing.T) {
	t.Parallel()
	db := store.OpenTestDB(t).DB

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
	workdirBase := t.TempDir()
	workdirs := workdir.NewManager(workdirBase, "", "")
	outputs := sessionoutputs.NewStore(t.TempDir())
	files := store.NewFileRepo(db)
	fileBlobs := fileblob.NewStore(t.TempDir())
	models := &modelresolve.Resolver{Cards: modelCards}

	handler := api.NewRouter(api.Deps{
		Agents:         agents,
		Environments:   environments,
		ModelCards:     modelCards,
		Files:          files,
		FileBlobs:      fileBlobs,
		SessionOutputs: outputs,
		AuthDisabled:   true,
		Sessions: api.NewSessionHandlers(
			sessions, agents, events, pending, hub, reg, workdirs,
			outputs, files, fileBlobs, harness.DefaultOnly(&harness.FakeClient{}), harness.AsOutcomeEvaluator(&harness.FakeClient{}), models,
			&harness.ResourceResolver{},
			store.NewWakeupRepo(db),
			store.NewTeamRepo(db),
			nil,
			"", "", "", "", "", "", "",
		),
	})

	ctx := context.Background()
	agent, err := agents.Create(ctx, store.CreateAgentInput{
		TenantID: "default",
		Name:     "exec-agent",
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
	if _, err := workdirs.Ensure(ctx, "default", sess.ID, nil); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{
		"command":    "echo hello",
		"timeout_ms": 5000,
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/sessions/"+sess.ID+"/exec",
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Output string `json:"output"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Output != "hello" {
		t.Fatalf("output=%q", resp.Output)
	}
}
