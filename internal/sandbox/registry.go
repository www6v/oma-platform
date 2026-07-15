package sandbox

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// fallbackCooldown is how long the registry skips OpenSandbox after a
// failure before trying it again. Short enough that a recovered service is
// picked up quickly; long enough to avoid hammering a down service on
// every session create.
const fallbackCooldown = 30 * time.Second

// Registry holds per-session sandbox executors.
type Registry struct {
	cfg        Config
	httpClient *http.Client
	mu         sync.Mutex
	sessions   map[string]Executor

	// degradedUntil is a unix-nano timestamp. While time.Now() < this
	// value, OpenSandbox is assumed unavailable and AcquireWith returns
	// a local executor immediately (only when fallback is enabled).
	degradedUntil atomic.Int64
}

// NewRegistry returns a sandbox registry for the configured provider.
func NewRegistry(cfg Config) *Registry {
	return &Registry{
		cfg:        cfg,
		httpClient: http.DefaultClient,
		sessions:   make(map[string]Executor),
	}
}

// Provider returns the active provider name.
func (r *Registry) Provider() string {
	if r == nil {
		return ProviderLocal
	}
	if r.cfg.Provider == "" {
		return ProviderLocal
	}
	return r.cfg.Provider
}

// Config returns the registry configuration.
func (r *Registry) Config() Config {
	if r == nil {
		return Config{Provider: ProviderLocal}
	}
	return r.cfg
}

// Acquire returns the session executor, creating isolated sandboxes lazily.
// It uses the registry's global Config — callers that need per-environment
// resolution should call AcquireWith instead.
func (r *Registry) Acquire(ctx context.Context, opts AcquireOpts) (Executor, error) {
	if r == nil {
		return NewLocalExecutor(opts.WorkdirPath), nil
	}
	return r.AcquireWith(ctx, r.cfg, opts)
}

// AcquireWith returns the session executor using the supplied per-session
// Config. This is the path used when a session binds to a non-default
// Environment whose config selects a sandbox provider.
//
// The cfg argument fully determines the provider and its parameters — the
// registry's own global cfg is only used as a fallback when cfg is empty.
// The per-session cache is keyed by (sessionID, provider) so a session
// that resolves to a different provider than a prior lookup does not
// silently reuse the prior executor.
func (r *Registry) AcquireWith(
	ctx context.Context,
	cfg Config,
	opts AcquireOpts,
) (Executor, error) {
	if r == nil || (cfg.Provider == "" && (r.cfg.Provider == "" || !r.cfg.IsRemote())) {
		return NewLocalExecutor(opts.WorkdirPath), nil
	}
	if cfg.Provider == "" {
		cfg = r.cfg
	}
	if !cfg.IsRemote() {
		return NewLocalExecutor(opts.WorkdirPath), nil
	}

	r.mu.Lock()
	key := registryCacheKey(opts.SessionID, cfg.Provider)
	if ex, ok := r.sessions[key]; ok {
		r.mu.Unlock()
		return ex, nil
	}
	r.mu.Unlock()

	var ex Executor
	var err error
	switch cfg.Provider {
	case ProviderE2B:
		ex, err = NewE2BExecutor(ctx, cfg, opts.SessionID, r.httpClient)
	case ProviderDaytona:
		ex, err = NewDaytonaExecutor(ctx, cfg, r.httpClient)
	case ProviderLiteBox:
		ex, err = NewLiteBoxExecutor(ctx, cfg, opts)
	case ProviderBoxRun:
		ex, err = NewBoxRunExecutor(ctx, cfg, opts.SessionID, r.httpClient)
	case ProviderOpenSandbox:
		// Fast path: if fallback is enabled and a recent OpenSandbox
		// failure opened the circuit, skip the remote call entirely
		// and serve local. Avoids paying the (long) create timeout on
		// every session while the service is known-down.
		if cfg.OpenSandboxFallbackLocal &&
			r.degradedUntil.Load() > time.Now().UnixNano() {
			log.Printf(
				"sandbox: opensandbox circuit open, using local fallback (session=%s)",
				opts.SessionID,
			)
			ex = NewLocalExecutor(opts.WorkdirPath)
			break
		}
		ex, err = NewOpenSandboxExecutor(ctx, cfg, opts, r.httpClient)
		if err != nil && cfg.OpenSandboxFallbackLocal {
			log.Printf(
				"sandbox: opensandbox unavailable, falling back to local: %v (session=%s, cooldown=%s)",
				err, opts.SessionID, fallbackCooldown,
			)
			r.degradedUntil.Store(
				time.Now().Add(fallbackCooldown).UnixNano(),
			)
			ex = NewLocalExecutor(opts.WorkdirPath)
			err = nil
		}
	default:
		return NewLocalExecutor(opts.WorkdirPath), nil
	}
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.sessions[key] = ex
	r.mu.Unlock()
	return ex, nil
}

// registryCacheKey builds the per-session cache key. Today it includes the
// provider so that a session whose Environment resolves differently from a
// previous lookup doesn't silently reuse the wrong executor. If session→
// environment bindings ever become mutable, extend this key with the
// environment ID.
func registryCacheKey(sessionID, provider string) string {
	if provider == "" || provider == ProviderLocal {
		return sessionID
	}
	return sessionID + "|" + provider
}

// Get returns an existing executor or a local fallback. Provider-aware:
// returns a cached executor regardless of which provider produced it.
func (r *Registry) Get(sessionID, workdirPath string) Executor {
	if r == nil {
		return NewLocalExecutor(workdirPath)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// Scan keys with matching sessionID prefix (cache keys are
	// "sessionID" or "sessionID|provider").
	for k, ex := range r.sessions {
		if k == sessionID || strings.HasPrefix(k, sessionID+"|") {
			return ex
		}
	}
	return NewLocalExecutor(workdirPath)
}

// Release destroys and removes a session sandbox. Matches all cache keys
// for the session (today at most one, since session→environment is 1:1).
func (r *Registry) Release(ctx context.Context, sessionID string) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	// Collect matching keys and their executors atomically.
	var removed []Executor
	for k, ex := range r.sessions {
		if k == sessionID || strings.HasPrefix(k, sessionID+"|") {
			removed = append(removed, ex)
			delete(r.sessions, k)
		}
	}
	r.mu.Unlock()
	if len(removed) == 0 {
		return nil
	}
	// Destroy outside the lock — destroy may hit the network.
	var firstErr error
	for _, ex := range removed {
		if err := ex.Destroy(ctx); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("sandbox destroy: %w", err)
		}
	}
	return firstErr
}
