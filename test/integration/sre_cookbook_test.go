package integration_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/open-ma/oma-building/internal/api"
	"github.com/open-ma/oma-building/internal/dream"
	"github.com/open-ma/oma-building/internal/fileblob"
	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/harness/demo"
	"github.com/open-ma/oma-building/internal/integrationtest"
	"github.com/open-ma/oma-building/internal/modelresolve"
	"github.com/open-ma/oma-building/internal/oauthflow"
	"github.com/open-ma/oma-building/internal/runtime"
	"github.com/open-ma/oma-building/internal/session"
	"github.com/open-ma/oma-building/internal/sessionoutputs"
	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/stream"
	"github.com/open-ma/oma-building/internal/workdir"
)

// TestSreCookbookIncidentResponder is the Go-server parity probe for
// sre_incident_responder: skill inject, three file mounts, HITL approval.
func TestSreCookbookIncidentResponder(t *testing.T) {
	sim := &demo.SreSimulatingClient{}
	handler := newSreCookbookRouter(t, sim)
	integrationtest.RunSreIncidentResponderFlow(t, handler, sim)
}

func newSreCookbookRouter(
	t *testing.T,
	client harness.Client,
) http.Handler {
	t.Helper()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	ctx := context.Background()
	agents := store.NewAgentRepo(db)
	environments := store.NewEnvironmentRepo(db)
	if err := environments.EnsureDefault(ctx); err != nil {
		t.Fatal(err)
	}
	modelCards := store.NewModelCardRepo(db)
	files := store.NewFileRepo(db)
	fileBlobs := fileblob.NewStore(t.TempDir())
	skillFiles := store.NewSkillFileStore(t.TempDir())
	skills := store.NewSkillRepo(db, skillFiles)
	memoryStores := store.NewMemoryStoreRepo(db, nil)
	sessions := store.NewSessionRepo(db, agents, environments)
	events := store.NewEventRepo(db)
	pending := store.NewPendingRepo(db)
	hub := stream.NewHub()
	reg := session.NewRegistry()
	t.Cleanup(func() { reg.Shutdown() })
	workdirs := workdir.NewManager(t.TempDir(), "", "")
	outputs := sessionoutputs.NewStore(t.TempDir())
	models := &modelresolve.Resolver{Cards: modelCards}

	sessionHandlers := api.NewSessionHandlers(
		sessions, agents, events, pending, hub, reg, workdirs,
		outputs, files, fileBlobs, client, models,
		&harness.ResourceResolver{
			Files:        files,
			FileBlobs:    fileBlobs,
			Skills:       skills,
			SkillFiles:   skillFiles,
			MemoryStores: memoryStores,
		},
		store.NewWakeupRepo(db),
		store.NewTeamRepo(db),
		nil,
		"", "", "http://test", "test-internal-secret", "", "", "",
	)

	return api.NewRouter(api.Deps{
		Agents:         agents,
		Environments:   environments,
		ModelCards:     modelCards,
		Skills:         skills,
		SkillFiles:     skillFiles,
		Files:          files,
		FileBlobs:      fileBlobs,
		SessionOutputs: outputs,
		ApiKeys:        store.NewApiKeyRepo(db),
		Tenants:        store.NewTenantRepo(db),
		Integrations:   store.NewIntegrationRepo(db),
		Runtimes:       store.NewRuntimeRepo(db),
		RuntimeRooms:   runtime.NewRegistry(store.NewRuntimeRepo(db)),
		MemoryStores:   memoryStores,
		EvalRuns:       store.NewEvalRunRepo(db),
		Dreams:         store.NewDreamRepo(db),
		DreamWorker: &dream.Worker{
			Dreams:       store.NewDreamRepo(db),
			MemoryStores: memoryStores,
			Sessions:     sessions,
		},
		Events:         events,
		AuthDisabled:   true,
		Sessions:       sessionHandlers,
		ModelResolver:  models,
		InternalSecret: "test-internal-secret",
		OAuthState:     oauthflow.NewStateStore(),
		PublicURL:      "http://test",
	})
}
