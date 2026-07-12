package harness

import (
	"context"
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

// KnownAgents lists the managed-agent ids the platform may spawn. Used by
// the Agent API to reject unknown runtime_binding.agent values at create
// time (Phase 3), and by the Phase 4 pool to map ids to daemon images.
var KnownAgents = []string{
	"hermes",
	"openclaw",
	"claude-acp",
	"codex-acp",
}

// IsKnownAgent reports whether agent is in KnownAgents.
func IsKnownAgent(agent string) bool {
	for _, k := range KnownAgents {
		if k == agent {
			return true
		}
	}
	return false
}

// ManagedClient is the Phase 3 stub for harness=managed. RunTurn returns an
// error indicating the managed pool is not yet implemented (Phase 4 will
// replace this with a real pool-backed client).
type ManagedClient struct{}

// managedErr is the sentinel error returned by ManagedClient.RunTurn and
// surfaced as an HTTP 501-equivalent through failTurn.
const managedNotImplemented = "managed harness not implemented (Phase 4 pending)"

// RunTurn implements Client. Always returns managedNotImplemented.
func (ManagedClient) RunTurn(context.Context, TurnRequest) (TurnResponse, error) {
	return TurnResponse{}, fmt.Errorf("%s", managedNotImplemented)
}

// OpenClawConfig holds the configuration for the OpenClaw Gateway
// integration. When GatewayURL is empty the managed factory falls back to
// the ManagedClient stub so existing tests and deployments without
// OpenClaw continue to work unchanged.
type OpenClawConfig struct {
	// GatewayURL is the OpenClaw Gateway base URL, e.g.
	// "http://124.221.28.203:17772". No trailing slash.
	GatewayURL string
	// Token is the Bearer token for Gateway authentication.
	Token string
}

// HermesConfig holds the configuration for the Hermes Agent API server
// integration. Hermes uses an OpenAI-compatible HTTP endpoint
// (POST /v1/chat/completions) and is stateless — the full conversation
// history is sent with every request.
type HermesConfig struct {
	// GatewayURL is the Hermes API server base URL, e.g.
	// "http://124.221.28.203:8642". No trailing slash.
	GatewayURL string
	// Token is the Bearer token (API_SERVER_KEY) for auth.
	Token string
}

// NewOpenClawFactory returns a ManagedFactory that creates OpenClawClient
// instances per binding. The binding.Agent field maps to the OpenClaw
// model id:
//
//	"openclaw" → "openclaw/default"
//	"<other>"  → "openclaw/<other>"
//
// When cfg.GatewayURL is empty the factory returns the ManagedClient stub
// instead, preserving backward compatibility for tests and deployments
// that haven't configured an OpenClaw Gateway yet.
func NewOpenClawFactory(cfg OpenClawConfig) func(ManagedBinding) (Client, error) {
	return func(b ManagedBinding) (Client, error) {
		if cfg.GatewayURL == "" {
			return ManagedClient{}, nil
		}
		model := "openclaw/" + b.Agent
		if b.Agent == "openclaw" {
			model = "openclaw/default"
		}
		return &OpenClawClient{
			GatewayURL: cfg.GatewayURL,
			Token:      cfg.Token,
			Agent:      model,
		}, nil
	}
}

// NewHermesFactory returns a ManagedFactory that creates HermesClient
// instances per binding. The binding.Agent field maps to the Hermes model
// id — by default "hermes-agent" for all agents, but the binding may
// override via runtime_binding.model if needed in the future.
//
// When cfg.GatewayURL is empty the factory returns the ManagedClient stub.
func NewHermesFactory(cfg HermesConfig) func(ManagedBinding) (Client, error) {
	return func(b ManagedBinding) (Client, error) {
		if cfg.GatewayURL == "" {
			return ManagedClient{}, nil
		}
		return &HermesClient{
			GatewayURL: cfg.GatewayURL,
			Token:      cfg.Token,
			Model:      "hermes-agent",
		}, nil
	}
}

// NewManagedFactory returns a ManagedFactory that dispatches by
// binding.Agent — "hermes" routes to HermesConfig, everything else routes
// to OpenClawConfig. When both configs have empty GatewayURL the factory
// falls back to the ManagedClient stub.
func NewManagedFactory(oc OpenClawConfig, hc HermesConfig) func(ManagedBinding) (Client, error) {
	ocFactory := NewOpenClawFactory(oc)
	hFactory := NewHermesFactory(hc)
	return func(b ManagedBinding) (Client, error) {
		if b.Agent == "hermes" {
			return hFactory(b)
		}
		return ocFactory(b)
	}
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

// NewRegistry builds a Registry from cfg. When ManagedFactory is nil, the
// registry falls back to a ManagedClient stub whose RunTurn returns 501 —
// Phase 3 behavior. Phase 4 wires a real pool-backed factory.
func NewRegistry(cfg RegistryConfig) *Registry {
	fake := cfg.Fake
	if fake == nil {
		fake = &FakeClient{}
	}
	managedFactory := cfg.ManagedFactory
	if managedFactory == nil {
		managedFactory = func(ManagedBinding) (Client, error) {
			return ManagedClient{}, nil
		}
	}
	return &Registry{
		defaultClient:  cfg.Default,
		managedFactory: managedFactory,
		fakeClient:     fake,
		forceClient:    cfg.Force,
	}
}

// DefaultOnly builds a Registry that returns defaultClient for every agent
// regardless of _oma.harness. Use this to migrate existing tests with a
// 1-line change: `Harness: c` → `HarnessRegistry: DefaultOnly(c)`.
// Managed kind falls back to the ManagedClient stub.
func DefaultOnly(defaultClient Client) *Registry {
	return &Registry{
		defaultClient: defaultClient,
		managedFactory: func(ManagedBinding) (Client, error) {
			return ManagedClient{}, nil
		},
		fakeClient: &FakeClient{},
	}
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
