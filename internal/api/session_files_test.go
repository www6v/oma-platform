package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func TestPromoteSandboxFile(t *testing.T) {
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
			outputs, files, fileBlobs, &harness.FakeClient{}, models,
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
		Name:     "promote-agent",
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
	sessionDir := filepath.Join(workdirBase, sess.ID)
	greetingPath := filepath.Join(sessionDir, "greeting.txt")
	if err := os.WriteFile(
		greetingPath,
		[]byte("hello-from-sandbox"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(map[string]any{
		"path":         "/workspace/greeting.txt",
		"filename":     "greeting.txt",
		"media_type":   "text/plain",
		"downloadable": true,
	})
	url := "/v1/sessions/" + sess.ID + "/files"
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var fileRec map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &fileRec); err != nil {
		t.Fatal(err)
	}
	if fileRec["type"] != "file" {
		t.Fatalf("type=%v", fileRec["type"])
	}
	fileID, _ := fileRec["id"].(string)
	if fileID == "" {
		t.Fatal("missing file id")
	}

	contentReq := httptest.NewRequest(
		http.MethodGet,
		"/v1/files/"+fileID+"/content",
		nil,
	)
	contentRec := httptest.NewRecorder()
	handler.ServeHTTP(contentRec, contentReq)
	if contentRec.Code != http.StatusOK {
		t.Fatalf("content status=%d", contentRec.Code)
	}
	if contentRec.Body.String() != "hello-from-sandbox" {
		t.Fatalf("content=%q", contentRec.Body.String())
	}
}

func TestSessionDeleteRemovesWorkdir(t *testing.T) {
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
			outputs, files, fileBlobs, &harness.FakeClient{}, models,
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
		Name:     "delete-agent",
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
	sessionDir := filepath.Join(workdirBase, sess.ID)
	if _, err := workdirs.Ensure(ctx, "default", sess.ID, nil); err != nil {
		t.Fatal(err)
	}

	delReq := httptest.NewRequest(
		http.MethodDelete,
		"/v1/sessions/"+sess.ID,
		nil,
	)
	delRec := httptest.NewRecorder()
	handler.ServeHTTP(delRec, delReq)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", delRec.Code, delRec.Body.String())
	}
	if _, err := os.Stat(sessionDir); !os.IsNotExist(err) {
		t.Fatalf("workdir still exists: %v", err)
	}
}
