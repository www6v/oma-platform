package api_test

import (
	"context"
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

func TestSessionDeleteSnapshotsWorkspace(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

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
	backupRepo := store.NewWorkspaceBackupRepo(db)
	fileBlobs := fileblob.NewStore(t.TempDir())
	workdirs.Backup = workdir.NewBackupService(backupRepo, fileBlobs)
	outputs := sessionoutputs.NewStore(t.TempDir())
	files := store.NewFileRepo(db)
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
		Name:     "backup-agent",
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
	if err := os.WriteFile(
		filepath.Join(sessionDir, "artifact.txt"),
		[]byte("saved"),
		0o644,
	); err != nil {
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

	row, err := backupRepo.FindLatest(
		ctx, "default", store.DefaultEnvironmentID, sess.ID,
	)
	if err != nil {
		t.Fatal(err)
	}
	if row == nil {
		t.Fatal("expected backup row after delete")
	}
	data, err := fileBlobs.ReadKey(row.Handle.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Fatal("empty backup blob")
	}
}
