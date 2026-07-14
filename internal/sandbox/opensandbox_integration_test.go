package sandbox_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/sandbox"
)

// TestOpenSandboxIntegration is a live e2e test that hits a real OpenSandbox
// Lifecycle Server. Gated by OMA_OPENSANDBOX_E2E=1 so it doesn't run in
// regular CI — only in the smoke script or when a developer opts in.
//
// Required env:
//
//	OPENSANDBOX_DOMAIN  (e.g. "124.221.28.203:18090")
// Optional:
//	OPENSANDBOX_PROTOCOL (default "http")
//	OPENSANDBOX_API_KEY
//	OPENSANDBOX_IMAGE   (default "python:3.12")
func TestOpenSandboxIntegration(t *testing.T) {
	if os.Getenv("OMA_OPENSANDBOX_E2E") != "1" {
		t.Skip("set OMA_OPENSANDBOX_E2E=1 to run live OpenSandbox e2e")
	}
	domain := os.Getenv("OPENSANDBOX_DOMAIN")
	if domain == "" {
		t.Skip("OPENSANDBOX_DOMAIN not set")
	}
	proto := os.Getenv("OPENSANDBOX_PROTOCOL")
	if proto == "" {
		proto = "http"
	}
	cfg := sandbox.Config{
		Provider:                sandbox.ProviderOpenSandbox,
		OpenSandboxDomain:       domain,
		OpenSandboxProtocol:     proto,
		OpenSandboxAPIKey:       os.Getenv("OPENSANDBOX_API_KEY"),
		OpenSandboxUseServerProxy: true,
		OpenSandboxImage:        envOr("OPENSANDBOX_IMAGE", "python:3.12"),
		OpenSandboxTimeoutSec:   600,
		OpenSandboxCPU:          "500m",
		OpenSandboxMemory:       "256Mi",
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	sessionID := fmt.Sprintf("go-e2e-%d", time.Now().UnixNano())
	t.Logf("creating sandbox for session %s", sessionID)

	ex, err := sandbox.NewOpenSandboxExecutor(ctx, cfg,
		sandbox.AcquireOpts{SessionID: sessionID, TenantID: "e2e"},
		nil, // default http client
	)
	if err != nil {
		t.Fatalf("NewOpenSandboxExecutor: %v", err)
	}
	defer func() {
		dctx, dcancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer dcancel()
		if err := ex.Destroy(dctx); err != nil {
			t.Errorf("Destroy: %v", err)
		}
	}()

	if ex.Provider() != sandbox.ProviderOpenSandbox {
		t.Fatalf("provider=%q", ex.Provider())
	}

	// Exec: echo marker + uname to prove we're inside Linux.
	marker := "e2e-" + sessionID
	execCtx, execCancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer execCancel()
	out, err := ex.Exec(execCtx,
		fmt.Sprintf("echo %s && uname -s && pwd", marker), 60*time.Second)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if !strings.Contains(out, marker) {
		t.Fatalf("output missing marker: %q", out)
	}
	if !strings.Contains(out, "Linux") {
		t.Fatalf("output missing 'Linux': %q", out)
	}
	t.Logf("Exec OK: %q", strings.TrimSpace(out))

	// Exec: write + read a file via /workspace.
	if _, err := ex.Exec(context.Background(),
		"echo hello-e2e > /workspace/smoke.txt", 30*time.Second); err != nil {
		t.Fatalf("write exec: %v", err)
	}
	got, err := ex.ReadFile(context.Background(), "/workspace/smoke.txt")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !strings.Contains(string(got), "hello-e2e") {
		t.Fatalf("ReadFile content=%q", got)
	}
	t.Logf("ReadFile OK: %q", strings.TrimSpace(string(got)))

	// Exec: non-zero exit should surface as "[exit N]" suffix, no Go error.
	out, err = ex.Exec(context.Background(), "exit 13", 30*time.Second)
	if err != nil {
		t.Fatalf("Exec non-zero: %v", err)
	}
	if !strings.HasSuffix(out, "[exit 13]") {
		t.Fatalf("expected [exit 13] suffix, got %q", out)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
