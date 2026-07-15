package session_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/sandbox"
	"github.com/open-ma/oma-building/internal/session"
	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/stream"
	"github.com/open-ma/oma-building/internal/workdir"
)

// TestMachine_PerEnvironmentSandboxBinding is an integration test that
// exercises the full path:
//
//   Environment (in SQLite) → Resolver → Registry.AcquireWith → fake
//   OpenSandbox lifecycle HTTP server.
//
// It asserts that when a session is bound to an environment whose config
// specifies an OpenSandbox image, the create request received by the
// lifecycle server carries that image — NOT the global default. This is
// the core guarantee of the per-environment binding feature.
func TestMachine_PerEnvironmentSandboxBinding(t *testing.T) {
	// 1. In-memory SQLite with two environments: the default (local) and
	//    a custom one bound to OpenSandbox with an env-specific image.
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	agents := store.NewAgentRepo(db)
	environments := store.NewEnvironmentRepo(db)
	if err := environments.EnsureDefault(context.Background()); err != nil {
		t.Fatal(err)
	}

	const envSpecificImage = "env-specific-image:v1"
	envCfg, err := json.Marshal(map[string]any{
		"type": "sandbox",
		"sandbox": map[string]any{
			"provider": "opensandbox",
			"opensandbox": map[string]any{
				// domain is filled in below once we know the
				// httptest server URL.
				"image": envSpecificImage,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	// 2. Fake OpenSandbox lifecycle + execd servers. The lifecycle server
	//    captures the create request body so we can assert on the image
	//    field; the execd server speaks just enough /ping + /command SSE
	//    to let NewOpenSandboxExecutor succeed and RunTurn make progress.
	f := newSessionFakeOpenSandbox(t)

	// Re-marshal the env config now that we know the lifecycle domain.
	domain := strings.TrimPrefix(f.lifecycle.URL, "http://")
	domain = strings.TrimPrefix(domain, "https://")
	envCfg, err = json.Marshal(map[string]any{
		"type": "sandbox",
		"sandbox": map[string]any{
			"provider": "opensandbox",
			"opensandbox": map[string]any{
				"domain": domain,
				"image":  envSpecificImage,
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	customEnv, err := environments.Create(context.Background(),
		store.CreateEnvironmentInput{
			TenantID: "default",
			Name:     "opensandbox-env",
			Config:   envCfg,
		})
	if err != nil {
		t.Fatal(err)
	}

	// 3. Agent + session bound to the custom environment.
	sessions := store.NewSessionRepo(db, agents, environments)
	events := store.NewEventRepo(db)
	pending := store.NewPendingRepo(db)
	hub := stream.NewHub()
	workdirs := workdir.NewManager(t.TempDir(), "", "")
	ctx := context.Background()

	agent, err := agents.Create(ctx, store.CreateAgentInput{
		Name:  "env-binding-agent",
		Model: "faux/test",
	})
	if err != nil {
		t.Fatal(err)
	}
	sess, err := sessions.Create(ctx, store.CreateSessionInput{
		AgentID:       agent.ID,
		EnvironmentID: customEnv.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// 4. Sandbox resolver + registry. The global cfg points at a DUMMY
	//    domain that must NOT be contacted — the env config overrides it.
	globalCfg := sandbox.Config{
		Provider:          sandbox.ProviderOpenSandbox,
		OpenSandboxDomain: "should-not-be-contacted.invalid:18090",
		OpenSandboxImage:  "python:3.12-slim", // global default
	}
	resolver := sandbox.NewResolver(globalCfg)
	registry := sandbox.NewRegistry(globalCfg)
	workdirs.Sandbox = registry

	// 5. Machine with the new dependencies.
	machine := &session.Machine{
		TenantID:        "default",
		SessionID:       sess.ID,
		Sessions:        sessions,
		Agents:          agents,
		Events:          events,
		Pending:         pending,
		Environments:    environments,
		Hub:             hub,
		Workdirs:        workdirs,
		HarnessRegistry: harness.DefaultOnly(&harness.FakeClient{Text: "hi"}),
		SandboxResolver: resolver,
	}

	userEvent, _ := json.Marshal(map[string]any{
		"type":    "user.message",
		"content": []map[string]string{{"type": "text", "text": "hi"}},
	})
	if _, err := events.AppendEvents(ctx, sess.ID,
		[]json.RawMessage{userEvent}); err != nil {
		t.Fatal(err)
	}

	// 6. Run the turn. This triggers:
	//    loadEnvironmentView → Resolver.Resolve → AcquireWith → HTTP POST
	//    /v1/sandboxes on the fake lifecycle server.
	if err := machine.RunTurn(ctx, "sthr_primary"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	// 7. Verify the captured create request used the env-specific image,
	//    NOT the global default.
	body := f.CreateBody()
	if body == nil {
		t.Fatal("lifecycle never received POST /v1/sandboxes")
	}
	if body.Image.URI != envSpecificImage {
		t.Fatalf("image=%q, want %q (env-specific); global leak suspect",
			body.Image.URI, envSpecificImage)
	}

	// Bonus: confirm the lifecycle server saw exactly one create (no
	// retries, no duplicate acquisitions).
	if got := f.CreateCount(); got != 1 {
		t.Fatalf("create count=%d, want 1", got)
	}
}

// --- inline fake OpenSandbox -------------------------------------------------
//
// Minimal lifecycle + execd pair, scoped to this test. The lifecycle
// records the create request body so the test can assert on it; the execd
// speaks just enough SSE to satisfy NewOpenSandboxExecutor.

type sessionFakeOpenSandbox struct {
	lifecycle   *httptest.Server
	execd       *httptest.Server
	mu          sync.Mutex
	createCount int
	createBody  *fakeCreateBody
}

type fakeCreateBody struct {
	Image struct {
		URI string `json:"uri"`
	} `json:"image"`
}

func newSessionFakeOpenSandbox(t *testing.T) *sessionFakeOpenSandbox {
	t.Helper()
	f := &sessionFakeOpenSandbox{}

	f.execd = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/ping":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/command":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(w, "data: {\"type\":\"stdout\",\"text\":\"ok\\n\"}\n")
			_, _ = fmt.Fprintf(w, "data: {\"type\":\"execution_complete\",\"exit_code\":0,\"execution_time\":1}\n")
		case r.Method == http.MethodGet && r.URL.Path == "/files/download":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))

	f.lifecycle = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sandboxes":
			var body fakeCreateBody
			_ = json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.createCount++
			f.createBody = &body
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":        "sbx-test",
				"status":    map[string]string{"state": "Running"},
				"createdAt": time.Now().UTC().Format(time.RFC3339),
			})
		case r.Method == http.MethodGet &&
			strings.HasPrefix(r.URL.Path, "/v1/sandboxes/sbx-test/endpoints/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"endpoint": f.execd.URL + "/",
				"headers":  map[string]string{"X-Proxy-Sandbox-Id": "sbx-test"},
			})
		case r.Method == http.MethodDelete &&
			strings.HasPrefix(r.URL.Path, "/v1/sandboxes/sbx-test"):
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))

	t.Cleanup(func() {
		f.lifecycle.Close()
		f.execd.Close()
	})
	return f
}

func (f *sessionFakeOpenSandbox) CreateBody() *fakeCreateBody {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.createBody
}

func (f *sessionFakeOpenSandbox) CreateCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.createCount
}

// TestMachine_DefaultEnvironment_StaysLocalWhenGlobalIsRemote asserts the
// backward-compat invariant: when a session is bound to the default
// environment ({"type":"local"}), the Machine uses the local provider
// even if the deployment's global SANDBOX_PROVIDER is opensandbox. The
// fake lifecycle server must NEVER be contacted in this case.
func TestMachine_DefaultEnvironment_StaysLocalWhenGlobalIsRemote(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	agents := store.NewAgentRepo(db)
	environments := store.NewEnvironmentRepo(db)
	if err := environments.EnsureDefault(context.Background()); err != nil {
		t.Fatal(err)
	}
	sessions := store.NewSessionRepo(db, agents, environments)
	events := store.NewEventRepo(db)
	pending := store.NewPendingRepo(db)
	hub := stream.NewHub()
	workdirs := workdir.NewManager(t.TempDir(), "", "")
	ctx := context.Background()

	agent, err := agents.Create(ctx, store.CreateAgentInput{
		Name:  "default-env-agent",
		Model: "faux/test",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Session with no explicit environment_id → store fills in the
	// default environment ("env-local-default" with config {"type":"local"}).
	sess, err := sessions.Create(ctx, store.CreateSessionInput{
		AgentID: agent.ID,
	})
	if err != nil {
		t.Fatal(err)
	}

	// A "live" fake lifecycle server that would record any hit. If the
	// Machine correctly honours the default environment's local type,
	// this server is never contacted.
	f := newSessionFakeOpenSandbox(t)

	// Global cfg says opensandbox — but the default environment must
	// override it back to local.
	globalCfg := sandbox.Config{
		Provider:          sandbox.ProviderOpenSandbox,
		OpenSandboxDomain: strings.TrimPrefix(f.lifecycle.URL, "http://"),
		OpenSandboxImage:  "python:3.12-slim",
	}
	resolver := sandbox.NewResolver(globalCfg)
	registry := sandbox.NewRegistry(globalCfg)
	workdirs.Sandbox = registry

	machine := &session.Machine{
		TenantID:        "default",
		SessionID:       sess.ID,
		Sessions:        sessions,
		Agents:          agents,
		Events:          events,
		Pending:         pending,
		Environments:    environments,
		Hub:             hub,
		Workdirs:        workdirs,
		HarnessRegistry: harness.DefaultOnly(&harness.FakeClient{Text: "hi"}),
		SandboxResolver: resolver,
	}

	userEvent, _ := json.Marshal(map[string]any{
		"type":    "user.message",
		"content": []map[string]string{{"type": "text", "text": "hi"}},
	})
	if _, err := events.AppendEvents(ctx, sess.ID,
		[]json.RawMessage{userEvent}); err != nil {
		t.Fatal(err)
	}

	if err := machine.RunTurn(ctx, "sthr_primary"); err != nil {
		t.Fatalf("RunTurn: %v", err)
	}

	// The lifecycle server must NOT have been contacted — default env's
	// type=local resolves to Provider=local, so no sandbox create.
	if got := f.CreateCount(); got != 0 {
		t.Fatalf("default environment should not trigger sandbox create, "+
			"got %d create calls", got)
	}
}

