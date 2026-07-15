package sandbox

import (
	"context"
	"net/http"
	"testing"
	"time"
)

// TestOpenSandboxFallback_OnFailure verifies that when OpenSandbox is
// unreachable and fallback is enabled, AcquireWith returns a local executor
// instead of an error and opens the degraded circuit.
func TestOpenSandboxFallback_OnFailure(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Provider:                 ProviderOpenSandbox,
		OpenSandboxDomain:        "invalid.test:18090", // DNS NXDOMAIN → fast fail
		OpenSandboxProtocol:      "http",
		OpenSandboxUseServerProxy: true,
		OpenSandboxExecdPort:     44772,
		OpenSandboxTimeoutSec:    5,
		OpenSandboxFallbackLocal: true,
	}
	r := NewRegistry(cfg)
	r.httpClient = &http.Client{Timeout: 2 * time.Second}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ex, err := r.AcquireWith(ctx, cfg, AcquireOpts{
		SessionID:    "sess-fallback-1",
		WorkdirPath:  t.TempDir(),
	})
	if err != nil {
		t.Fatalf("expected fallback to local, got error: %v", err)
	}
	if _, ok := ex.(*LocalExecutor); !ok {
		t.Fatalf("expected *LocalExecutor, got %T", ex)
	}

	// Circuit should now be open.
	if r.degradedUntil.Load() <= time.Now().UnixNano() {
		t.Fatal("expected degradedUntil to be in the future after a failure")
	}
}

// TestOpenSandboxFallback_CircuitSkipsRemote verifies that while the circuit
// is open, subsequent acquires use local without hitting the network. We
// assert this by timing the second call — local executor creation is
// microseconds, so a call that takes <1ms did not touch the network.
func TestOpenSandboxFallback_CircuitSkipsRemote(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Provider:                 ProviderOpenSandbox,
		OpenSandboxDomain:        "127.0.0.1:1", // connect refused
		OpenSandboxProtocol:      "http",
		OpenSandboxUseServerProxy: true,
		OpenSandboxExecdPort:     44772,
		OpenSandboxTimeoutSec:    30,
		OpenSandboxFallbackLocal: true,
	}
	r := NewRegistry(cfg)
	r.httpClient = &http.Client{Timeout: 2 * time.Second}

	ctx := context.Background()
	opts := AcquireOpts{SessionID: "sess-circuit-1", WorkdirPath: t.TempDir()}

	// Prime the circuit with a failing acquire.
	if _, err := r.AcquireWith(ctx, cfg, opts); err != nil {
		t.Fatalf("first acquire should fall back: %v", err)
	}
	if r.degradedUntil.Load() <= time.Now().UnixNano() {
		t.Fatal("expected degradedUntil to be in the future after a failure")
	}

	// Second acquire should be near-instant because the circuit bypasses
	// the remote call entirely.
	opts2 := AcquireOpts{SessionID: "sess-circuit-2", WorkdirPath: t.TempDir()}
	t0 := time.Now()
	ex, err := r.AcquireWith(ctx, cfg, opts2)
	secondDur := time.Since(t0)
	if err != nil {
		t.Fatalf("second acquire should fall back: %v", err)
	}
	if _, ok := ex.(*LocalExecutor); !ok {
		t.Fatalf("expected *LocalExecutor, got %T", ex)
	}
	// Local executor creation is microseconds. If this took >1ms, we
	// probably hit the network, meaning the circuit did not bypass.
	if secondDur > time.Millisecond {
		t.Fatalf(
			"second acquire took %s (expected <1ms); circuit did not bypass remote",
			secondDur,
		)
	}
}

// TestOpenSandboxFallback_Disabled verifies that when fallback is disabled,
// an unreachable OpenSandbox returns an error (existing behavior preserved).
func TestOpenSandboxFallback_Disabled(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Provider:                 ProviderOpenSandbox,
		OpenSandboxDomain:        "invalid.test:18090",
		OpenSandboxProtocol:      "http",
		OpenSandboxUseServerProxy: true,
		OpenSandboxExecdPort:     44772,
		OpenSandboxTimeoutSec:    5,
		OpenSandboxFallbackLocal: false,
	}
	r := NewRegistry(cfg)
	r.httpClient = &http.Client{Timeout: 2 * time.Second}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := r.AcquireWith(ctx, cfg, AcquireOpts{
		SessionID:   "sess-nofallback",
		WorkdirPath: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error when fallback disabled, got nil")
	}
}

// TestOpenSandboxFallback_CircuitExpires verifies that once the cooldown
// elapses, the next acquire re-tries OpenSandbox rather than staying on
// local forever.
func TestOpenSandboxFallback_CircuitExpires(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Provider:                 ProviderOpenSandbox,
		OpenSandboxDomain:        "invalid.test:18090",
		OpenSandboxProtocol:      "http",
		OpenSandboxUseServerProxy: true,
		OpenSandboxExecdPort:     44772,
		OpenSandboxTimeoutSec:    5,
		OpenSandboxFallbackLocal: true,
	}
	r := NewRegistry(cfg)
	r.httpClient = &http.Client{Timeout: 2 * time.Second}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := r.AcquireWith(ctx, cfg, AcquireOpts{
		SessionID: "sess-expire-1", WorkdirPath: t.TempDir(),
	}); err != nil {
		t.Fatalf("first acquire should fall back: %v", err)
	}

	// Simulate cooldown expiry by backdating degradedUntil.
	r.degradedUntil.Store(time.Now().Add(-time.Second).UnixNano())

	// Next acquire should attempt OpenSandbox again (and fail → fall back
	// again, which re-opens the circuit). We can't easily assert the
	// attempt directly without a hook, but we can assert that after the
	// call, degradedUntil is in the future (proving a fresh failure was
	// observed and the circuit was reset).
	if _, err := r.AcquireWith(ctx, cfg, AcquireOpts{
		SessionID: "sess-expire-2", WorkdirPath: t.TempDir(),
	}); err != nil {
		t.Fatalf("post-cooldown acquire should fall back: %v", err)
	}
	if r.degradedUntil.Load() <= time.Now().UnixNano() {
		t.Fatal("expected degradedUntil to be reset to the future after a fresh failure")
	}
}
