package sandbox

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegistryCacheKey_LocalOmitsProviderSuffix(t *testing.T) {
	if got := registryCacheKey("sess-1", ""); got != "sess-1" {
		t.Fatalf("empty provider: got %q", got)
	}
	if got := registryCacheKey("sess-1", ProviderLocal); got != "sess-1" {
		t.Fatalf("local provider: got %q", got)
	}
}

func TestRegistryCacheKey_RemoteIncludesProviderSuffix(t *testing.T) {
	got := registryCacheKey("sess-1", ProviderOpenSandbox)
	want := "sess-1|opensandbox"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// recordingExecutor captures Destroy calls so we can verify Release walks
// the right cache entries.
type recordingExecutor struct {
	provider  string
	destroyed atomic.Int32
}

func (r *recordingExecutor) Provider() string { return r.provider }
func (r *recordingExecutor) Exec(context.Context, string, time.Duration) (string, error) {
	return "", nil
}
func (r *recordingExecutor) ReadFile(context.Context, string) ([]byte, error) {
	return nil, nil
}
func (r *recordingExecutor) Destroy(context.Context) error {
	r.destroyed.Add(1)
	return nil
}

func TestRegistry_Release_MatchesProviderSuffixedKeys(t *testing.T) {
	// Build a registry and inject cache entries directly. This bypasses
	// Acquire (which would try to construct real executors) so we can
	// focus on the Release path's key-matching logic.
	reg := NewRegistry(Config{Provider: ProviderLocal})
	ex1 := &recordingExecutor{provider: ProviderOpenSandbox}
	ex2 := &recordingExecutor{provider: ProviderOpenSandbox}
	exOther := &recordingExecutor{provider: ProviderOpenSandbox}

	reg.mu.Lock()
	reg.sessions["sess-A|opensandbox"] = ex1
	reg.sessions["sess-B|opensandbox"] = ex2
	reg.sessions["sess-C|opensandbox"] = exOther
	reg.mu.Unlock()

	if err := reg.Release(context.Background(), "sess-A"); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if ex1.destroyed.Load() != 1 {
		t.Fatalf("sess-A executor not destroyed (count=%d)", ex1.destroyed.Load())
	}
	if ex2.destroyed.Load() != 0 {
		t.Fatalf("sess-B executor should not be destroyed")
	}
	if exOther.destroyed.Load() != 0 {
		t.Fatalf("sess-C executor should not be destroyed")
	}

	// Confirm sess-A entry is gone but others remain.
	reg.mu.Lock()
	_, aLeft := reg.sessions["sess-A|opensandbox"]
	_, bLeft := reg.sessions["sess-B|opensandbox"]
	_, cLeft := reg.sessions["sess-C|opensandbox"]
	reg.mu.Unlock()
	if aLeft {
		t.Fatalf("sess-A entry should be removed")
	}
	if !bLeft || !cLeft {
		t.Fatalf("sess-B/C entries should remain")
	}
}

func TestRegistry_Release_NoMatchIsNoop(t *testing.T) {
	reg := NewRegistry(Config{Provider: ProviderLocal})
	// Release on an empty registry must not error.
	if err := reg.Release(context.Background(), "sess-none"); err != nil {
		t.Fatalf("Release on empty: %v", err)
	}
	// Release on a nil registry must not error.
	var nilReg *Registry
	if err := nilReg.Release(context.Background(), "sess-none"); err != nil {
		t.Fatalf("Release on nil: %v", err)
	}
}

func TestRegistry_Get_MatchesProviderSuffixedKeys(t *testing.T) {
	reg := NewRegistry(Config{Provider: ProviderLocal})
	want := Executor(&recordingExecutor{provider: ProviderOpenSandbox})
	reg.mu.Lock()
	reg.sessions["sess-A|opensandbox"] = want
	reg.mu.Unlock()

	got := reg.Get("sess-A", "/tmp/work")
	if got != want {
		t.Fatalf("Get did not find provider-suffixed entry")
	}

	// Unknown session falls back to local.
	local := reg.Get("sess-missing", "/tmp/work")
	if local == nil {
		t.Fatalf("Get returned nil for missing session")
	}
	if local.Provider() != ProviderLocal {
		t.Fatalf("expected local fallback, got %q", local.Provider())
	}
}

func TestRegistry_AcquireWith_LocalProviderReturnsLocalExecutor(t *testing.T) {
	// Even when global cfg says remote, AcquireWith with a local cfg
	// should return a LocalExecutor without attempting sandbox creation.
	reg := NewRegistry(Config{
		Provider:          ProviderOpenSandbox,
		OpenSandboxDomain: "should-not-be-called.example.com:18090",
	})
	ex, err := reg.AcquireWith(
		context.Background(),
		Config{Provider: ProviderLocal},
		AcquireOpts{SessionID: "sess-1", WorkdirPath: "/tmp/w"},
	)
	if err != nil {
		t.Fatalf("AcquireWith: %v", err)
	}
	if ex.Provider() != ProviderLocal {
		t.Fatalf("expected local, got %q", ex.Provider())
	}
}

func TestRegistry_AcquireWith_NilRegistryReturnsLocal(t *testing.T) {
	var reg *Registry
	ex, err := reg.AcquireWith(
		context.Background(),
		Config{Provider: ProviderOpenSandbox, OpenSandboxDomain: "x:18090"},
		AcquireOpts{SessionID: "sess-1", WorkdirPath: "/tmp/w"},
	)
	if err != nil {
		t.Fatalf("AcquireWith nil: %v", err)
	}
	if ex.Provider() != ProviderLocal {
		t.Fatalf("nil registry should fall back to local, got %q", ex.Provider())
	}
}

