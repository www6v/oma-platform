package sandbox_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/sandbox"
)

// fakeOpenSandbox spins up a lifecycle server + execd server that speak the
// minimal OpenSandbox surface used by OpenSandboxExecutor.
type fakeOpenSandbox struct {
	lifecycle    *httptest.Server
	execd        *httptest.Server
	sandboxID    string
	execdCalls   int32 // atomic counter for /command and /files/download
	deleteCalls  int32
	pingStatus   int32 // HTTP status returned by /ping (default 200)
	execStdout   string
	execExitCode int
	fileContent  []byte // returned by /files/download
	filesStatus  int    // default 200; set to non-2xx to trigger fallback
}

func newFakeOpenSandbox(t *testing.T) *fakeOpenSandbox {
	t.Helper()
	f := &fakeOpenSandbox{
		sandboxID:   "sbx-123",
		pingStatus:  http.StatusOK,
		filesStatus: http.StatusOK,
		execStdout:  "hello world\n",
		fileContent: []byte("file-body"),
	}

	f.execd = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&f.execdCalls, 1)
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/ping":
			w.WriteHeader(int(atomic.LoadInt32(&f.pingStatus)))
		case r.Method == http.MethodPost && r.URL.Path == "/command":
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprintf(w, "data: {\"type\":\"stdout\",\"text\":%q}\n", f.execStdout)
			_, _ = fmt.Fprintf(w, "data: {\"type\":\"execution_complete\",\"exit_code\":%d,\"execution_time\":1}\n", f.execExitCode)
		case r.Method == http.MethodGet && r.URL.Path == "/files/download":
			if f.filesStatus >= 400 {
				w.WriteHeader(f.filesStatus)
				return
			}
			_, _ = w.Write(f.fileContent)
		default:
			http.NotFound(w, r)
		}
	}))

	f.lifecycle = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/sandboxes":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":     f.sandboxID,
				"status": map[string]string{"state": "Running"},
			})
		case r.Method == http.MethodGet &&
			strings.HasPrefix(r.URL.Path, "/v1/sandboxes/"+f.sandboxID+"/endpoints/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"endpoint": f.execd.URL + "/",
				"headers":  map[string]string{"X-Proxy-Sandbox-Id": f.sandboxID},
			})
		case r.Method == http.MethodDelete &&
			strings.HasPrefix(r.URL.Path, "/v1/sandboxes/"+f.sandboxID):
			atomic.AddInt32(&f.deleteCalls, 1)
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

func (f *fakeOpenSandbox) cfg() sandbox.Config {
	// lifecycle URL looks like http://127.0.0.1:NNNN; strip scheme for Domain.
	domain := strings.TrimPrefix(f.lifecycle.URL, "http://")
	domain = strings.TrimPrefix(domain, "https://")
	return sandbox.Config{
		Provider:            sandbox.ProviderOpenSandbox,
		OpenSandboxDomain:   domain,
		OpenSandboxProtocol: "http",
	}
}

func TestOpenSandboxExecRoundTrip(t *testing.T) {
	t.Parallel()
	f := newFakeOpenSandbox(t)
	ex, err := sandbox.NewOpenSandboxExecutor(
		context.Background(), f.cfg(),
		sandbox.AcquireOpts{SessionID: "sess-1"},
		f.lifecycle.Client(),
	)
	if err != nil {
		t.Fatal(err)
	}
	out, err := ex.Exec(context.Background(), "echo hello", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if out != "hello world" {
		t.Fatalf("output=%q", out)
	}
	if ex.Provider() != sandbox.ProviderOpenSandbox {
		t.Fatalf("provider=%q", ex.Provider())
	}
	if err := ex.Destroy(context.Background()); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(&f.deleteCalls) != 1 {
		t.Fatalf("expected one delete, got %d", f.deleteCalls)
	}
}

func TestOpenSandboxExecNonZeroExit(t *testing.T) {
	t.Parallel()
	f := newFakeOpenSandbox(t)
	f.execStdout = "oops\n"
	f.execExitCode = 7
	ex, err := sandbox.NewOpenSandboxExecutor(
		context.Background(), f.cfg(),
		sandbox.AcquireOpts{SessionID: "sess-2"},
		f.lifecycle.Client(),
	)
	if err != nil {
		t.Fatal(err)
	}
	out, err := ex.Exec(context.Background(), "exit 7", 5*time.Second)
	if err != nil {
		t.Fatal("expected no error for non-zero exit, just appended code")
	}
	if !strings.HasSuffix(out, "[exit 7]") {
		t.Fatalf("expected [exit 7] suffix, got %q", out)
	}
	if !strings.Contains(out, "oops") {
		t.Fatalf("expected stdout preserved, got %q", out)
	}
}

func TestOpenSandboxCreateError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("nope"))
	}))
	defer srv.Close()

	domain := strings.TrimPrefix(srv.URL, "http://")
	cfg := sandbox.Config{
		Provider:            sandbox.ProviderOpenSandbox,
		OpenSandboxDomain:   domain,
		OpenSandboxProtocol: "http",
	}
	_, err := sandbox.NewOpenSandboxExecutor(
		context.Background(), cfg,
		sandbox.AcquireOpts{SessionID: "sess-x"},
		srv.Client(),
	)
	if err == nil {
		t.Fatal("expected create error")
	}
	if !strings.Contains(err.Error(), "status=403") {
		t.Fatalf("error=%v", err)
	}
}

func TestOpenSandboxExecdNotReadyTriggersCleanup(t *testing.T) {
	t.Parallel()
	f := newFakeOpenSandbox(t)
	atomic.StoreInt32(&f.pingStatus, http.StatusServiceUnavailable)

	// Use a context with a tight deadline so the waitForExecd loop exits
	// quickly (its 15s wall-clock budget would otherwise make this test slow).
	ctx, cancel := context.WithTimeout(context.Background(), 800*time.Millisecond)
	defer cancel()

	_, err := sandbox.NewOpenSandboxExecutor(
		ctx, f.cfg(),
		sandbox.AcquireOpts{SessionID: "sess-slow"},
		f.lifecycle.Client(),
	)
	if err == nil {
		t.Fatal("expected execd-not-ready error")
	}
	// Constructor must have cleaned up the leaked sandbox.
	if atomic.LoadInt32(&f.deleteCalls) == 0 {
		t.Fatal("expected sandbox to be destroyed on failure")
	}
}

func TestOpenSandboxReadFileHappyPath(t *testing.T) {
	t.Parallel()
	f := newFakeOpenSandbox(t)
	f.fileContent = []byte(`{"ok":true}`)
	ex, err := sandbox.NewOpenSandboxExecutor(
		context.Background(), f.cfg(),
		sandbox.AcquireOpts{SessionID: "sess-3"},
		f.lifecycle.Client(),
	)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ex.ReadFile(context.Background(), "/workspace/out.json")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != `{"ok":true}` {
		t.Fatalf("content=%q", got)
	}
}

func TestOpenSandboxReadFileFallbackToBase64(t *testing.T) {
	t.Parallel()
	f := newFakeOpenSandbox(t)
	f.filesStatus = http.StatusInternalServerError
	// The fallback calls Exec("base64 -w0 <path>") which hits /command;
	// return the base64 of the desired content as stdout.
	want := "fallback-content"
	f.execStdout = base64.StdEncoding.EncodeToString([]byte(want)) + "\n"

	ex, err := sandbox.NewOpenSandboxExecutor(
		context.Background(), f.cfg(),
		sandbox.AcquireOpts{SessionID: "sess-4"},
		f.lifecycle.Client(),
	)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ex.ReadFile(context.Background(), "/workspace/f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Fatalf("got=%q want=%q", got, want)
	}
}

func TestOpenSandboxDestroyIdempotent(t *testing.T) {
	t.Parallel()
	f := newFakeOpenSandbox(t)
	ex, err := sandbox.NewOpenSandboxExecutor(
		context.Background(), f.cfg(),
		sandbox.AcquireOpts{SessionID: "sess-5"},
		f.lifecycle.Client(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := ex.Destroy(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := ex.Destroy(context.Background()); err != nil {
		t.Fatal("second destroy should be no-op")
	}
	// sandboxID cleared after first Destroy; second call shouldn't hit server.
	if atomic.LoadInt32(&f.deleteCalls) != 1 {
		t.Fatalf("expected one delete, got %d", f.deleteCalls)
	}
}

func TestOpenSandboxValidateMissingDomain(t *testing.T) {
	t.Parallel()
	cfg := sandbox.Config{Provider: sandbox.ProviderOpenSandbox}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected missing-domain error")
	}
	cfg.OpenSandboxDomain = "127.0.0.1:18090"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cfg.IsRemote() {
		t.Fatal("opensandbox should be remote")
	}
}
