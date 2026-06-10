package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/store"
)

// stallHarness blocks RunTurn until release is called.
type stallHarness struct {
	inner       harness.Client
	enteredOnce sync.Once
	releaseOnce sync.Once
	finishOnce  sync.Once
	entered     chan struct{}
	finished    chan struct{}
	unblock     chan struct{}
}

func newStallHarness(inner harness.Client) *stallHarness {
	if inner == nil {
		inner = &harness.FakeClient{Text: "ok"}
	}
	return &stallHarness{
		inner:    inner,
		entered:  make(chan struct{}),
		finished: make(chan struct{}),
		unblock:  make(chan struct{}),
	}
}

func (s *stallHarness) signalEntered() {
	s.enteredOnce.Do(func() { close(s.entered) })
}

func (s *stallHarness) release() {
	s.releaseOnce.Do(func() { close(s.unblock) })
}

func (s *stallHarness) markFinished() {
	s.finishOnce.Do(func() { close(s.finished) })
}

func (s *stallHarness) RunTurn(
	ctx context.Context,
	req harness.TurnRequest,
) (harness.TurnResponse, error) {
	defer s.markFinished()
	s.signalEntered()
	select {
	case <-s.unblock:
		return s.inner.RunTurn(ctx, req)
	case <-ctx.Done():
		return harness.TurnResponse{}, ctx.Err()
	}
}

func (s *stallHarness) RunTurnStream(
	ctx context.Context,
	req harness.TurnRequest,
	onEvent harness.EventHandler,
) error {
	defer s.markFinished()
	s.signalEntered()
	select {
	case <-s.unblock:
		return harness.RunTurnStreaming(ctx, s.inner, req, onEvent)
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestStartupRecoveryOrphanRunningSession(t *testing.T) {
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "oma.db")
	db, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	recording := &harness.RecordingClient{
		FakeClient: harness.FakeClient{Text: "recovered"},
	}
	stall := newStallHarness(recording)
	t.Cleanup(func() { stall.release() })

	handler, _, sessions := testRouterSharedDB(t, db, stall)
	server := httptest.NewServer(handler)
	defer server.Close()

	sid := createAgentSession(t, server.Client(), server.URL)
	eventsURL := server.URL + "/v1/sessions/" + sid + "/events"
	postJSON(t, server.Client(), eventsURL,
		`{"events":[{"type":"user.message","content":[{"type":"text","text":"hi"}]}]}`,
		http.StatusAccepted)

	select {
	case <-stall.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for in-flight harness turn")
	}

	running := getSession(t, server.Client(), server.URL+"/v1/sessions/"+sid)
	if running["status"] != "running" {
		t.Fatalf("before recovery status=%v", running["status"])
	}

	n, err := sessions.RecoverRunning(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("recovered=%d want 1", n)
	}

	interrupted := getSession(t, server.Client(), server.URL+"/v1/sessions/"+sid)
	if interrupted["status"] != "interrupted" {
		t.Fatalf("after recovery status=%v", interrupted["status"])
	}

	got, err := sessions.Get(ctx, "default", sid)
	if err != nil {
		t.Fatal(err)
	}
	if got.TurnID != nil {
		t.Fatalf("turn_id=%v want nil after recovery", *got.TurnID)
	}

	postJSON(t, server.Client(), eventsURL,
		`{"events":[{"type":"user.message","content":[{"type":"text","text":"again"}]}]}`,
		http.StatusAccepted)

	stall.release()
	waitForHarnessTurns(t, recording, 2, 5*time.Second)

	idle := getSession(t, server.Client(), server.URL+"/v1/sessions/"+sid)
	if idle["status"] != "idle" {
		t.Fatalf("after new turn status=%v", idle["status"])
	}
}

func TestStartupRecoveryNoRunningSessions(t *testing.T) {
	ctx := context.Background()
	db := store.OpenTestDB(t)
	sessions := store.NewSessionRepo(
		db.DB,
		store.NewAgentRepo(db.DB),
		store.NewEnvironmentRepo(db.DB),
	)
	if err := store.NewEnvironmentRepo(db.DB).EnsureDefault(ctx); err != nil {
		t.Fatal(err)
	}

	n, err := sessions.RecoverRunning(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("recovered=%d want 0", n)
	}
}

func waitForHarnessTurns(
	t *testing.T,
	recording *harness.RecordingClient,
	want int,
	timeout time.Duration,
) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if recording.RequestCount() >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("harness turns=%d want >= %d", recording.RequestCount(), want)
}

func getSession(
	t *testing.T,
	client *http.Client,
	url string,
) map[string]any {
	t.Helper()
	resp, err := client.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s status=%d", url, resp.StatusCode)
	}
	var sess map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		t.Fatal(err)
	}
	return sess
}
