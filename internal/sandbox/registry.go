package sandbox

import (
	"context"
	"fmt"
	"net/http"
	"sync"
)

// Registry holds per-session sandbox executors.
type Registry struct {
	cfg        Config
	httpClient *http.Client
	mu         sync.Mutex
	sessions   map[string]Executor
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
func (r *Registry) Acquire(ctx context.Context, opts AcquireOpts) (Executor, error) {
	if r == nil || !r.cfg.IsRemote() {
		return NewLocalExecutor(opts.WorkdirPath), nil
	}
	r.mu.Lock()
	if ex, ok := r.sessions[opts.SessionID]; ok {
		r.mu.Unlock()
		return ex, nil
	}
	r.mu.Unlock()

	var ex Executor
	var err error
	switch r.cfg.Provider {
	case ProviderE2B:
		ex, err = NewE2BExecutor(ctx, r.cfg, opts.SessionID, r.httpClient)
	case ProviderDaytona:
		ex, err = NewDaytonaExecutor(ctx, r.cfg, r.httpClient)
	case ProviderLiteBox:
		ex, err = NewLiteBoxExecutor(ctx, r.cfg, opts)
	case ProviderBoxRun:
		ex, err = NewBoxRunExecutor(ctx, r.cfg, opts.SessionID, r.httpClient)
	case ProviderOpenSandbox:
		ex, err = NewOpenSandboxExecutor(ctx, r.cfg, opts, r.httpClient)
	default:
		return NewLocalExecutor(opts.WorkdirPath), nil
	}
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	r.sessions[opts.SessionID] = ex
	r.mu.Unlock()
	return ex, nil
}

// Get returns an existing executor or a local fallback.
func (r *Registry) Get(sessionID, workdirPath string) Executor {
	if r == nil {
		return NewLocalExecutor(workdirPath)
	}
	r.mu.Lock()
	ex, ok := r.sessions[sessionID]
	r.mu.Unlock()
	if ok {
		return ex
	}
	return NewLocalExecutor(workdirPath)
}

// Release destroys and removes a session sandbox.
func (r *Registry) Release(ctx context.Context, sessionID string) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	ex, ok := r.sessions[sessionID]
	if ok {
		delete(r.sessions, sessionID)
	}
	r.mu.Unlock()
	if !ok || ex == nil {
		return nil
	}
	if err := ex.Destroy(ctx); err != nil {
		return fmt.Errorf("sandbox destroy: %w", err)
	}
	return nil
}
