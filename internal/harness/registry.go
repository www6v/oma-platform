package harness

import (
	"encoding/json"
	"fmt"

	"github.com/open-ma/oma-building/internal/store"
)

// Kind identifies a harness implementation.
type Kind string

const (
	// KindDefaultLoop is the pipy HTTP sidecar. The legacy alias "pipy" is
	// accepted on input and normalized to KindDefaultLoop.
	KindDefaultLoop Kind = "default-loop"
	// KindManaged is the platform-hosted per-tenant daemon pool (see §10).
	KindManaged Kind = "managed"
	// KindFake is the test stub. OMA_FAKE_HARNESS env var resolves here.
	KindFake Kind = "fake"
)

// ManagedBinding is the parsed form of AgentConfig.RuntimeBinding when
// harness=managed.
type ManagedBinding struct {
	// Agent is the ACP agent id the platform should spawn
	// (hermes | openclaw | claude-acp | codex-acp).
	Agent string `json:"agent"`
}

// Registry resolves a harness.Client per agent based on the agent's
// `_oma.harness` metadata. One Registry is constructed at process start
// and threaded through every session.Machine.
type Registry struct {
	defaultClient Client
	managedFactory func(ManagedBinding) (Client, error)
	fakeClient    Client
	forceClient   Client // if non-nil, returned for every agent (env-var override)
}

// RegistryConfig holds the per-kind client factories.
type RegistryConfig struct {
	// Default is the client returned for KindDefaultLoop (and for agents
	// with no _oma.harness set, since "" normalizes to default-loop).
	Default Client
	// ManagedFactory builds a Client for a given ManagedBinding. Invoked
	// per-turn so the factory may hand out pooled / per-tenant clients.
	ManagedFactory func(ManagedBinding) (Client, error)
	// Fake is the client returned for KindFake. Defaults to &FakeClient{}
	// when nil.
	Fake Client
	// Force overrides all dispatch when set. Used by OMA_FAKE_HARNESS to
	// keep the legacy "every session uses this test client" behavior.
	Force Client
}

// NewRegistry builds a Registry from cfg.
func NewRegistry(cfg RegistryConfig) *Registry {
	fake := cfg.Fake
	if fake == nil {
		fake = &FakeClient{}
	}
	return &Registry{
		defaultClient:  cfg.Default,
		managedFactory: cfg.ManagedFactory,
		fakeClient:     fake,
		forceClient:    cfg.Force,
	}
}

// DefaultOnly builds a Registry that returns defaultClient for every agent
// regardless of _oma.harness. Use this to migrate existing tests with a
// 1-line change: `Harness: c` → `HarnessRegistry: DefaultOnly(c)`.
func DefaultOnly(defaultClient Client) *Registry {
	return &Registry{defaultClient: defaultClient, fakeClient: &FakeClient{}}
}

// ClientFor resolves the harness.Client for the given agent config.
func (r *Registry) ClientFor(agent store.AgentConfig) (Client, error) {
	if r.forceClient != nil {
		return r.forceClient, nil
	}
	kind := Kind(agent.Harness)
	switch kind {
	case "":
		kind = KindDefaultLoop
	case "pipy":
		kind = KindDefaultLoop
	}
	switch kind {
	case KindDefaultLoop:
		if r.defaultClient == nil {
			return nil, fmt.Errorf("harness registry: default-loop client not configured")
		}
		return r.defaultClient, nil
	case KindManaged:
		if r.managedFactory == nil {
			return nil, fmt.Errorf("harness registry: managed kind not configured (Phase 4 pending)")
		}
		b, err := ParseManagedBinding(agent.RuntimeBinding)
		if err != nil {
			return nil, err
		}
		return r.managedFactory(b)
	case KindFake:
		return r.fakeClient, nil
	default:
		return nil, fmt.Errorf("harness registry: unknown kind %q", kind)
	}
}

// ParseManagedBinding parses AgentConfig.RuntimeBinding for harness=managed.
func ParseManagedBinding(raw json.RawMessage) (ManagedBinding, error) {
	if len(raw) == 0 {
		return ManagedBinding{}, fmt.Errorf("managed harness requires runtime_binding with agent field")
	}
	var b ManagedBinding
	if err := json.Unmarshal(raw, &b); err != nil {
		return ManagedBinding{}, fmt.Errorf("parse managed runtime_binding: %w", err)
	}
	if b.Agent == "" {
		return ManagedBinding{}, fmt.Errorf("managed harness requires runtime_binding.agent")
	}
	return b, nil
}
