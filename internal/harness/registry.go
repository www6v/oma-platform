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
	// KindDefaultLoop is the piPy HTTP sidecar. The legacy alias "pipy" is
	// accepted on input and normalized to KindDefaultLoop.
	KindDefaultLoop Kind = "default-loop"
	// KindManaged is the legacy two-layer kind. Agents written before the
	// 2026-08 flattening carry harness="managed" plus runtime_binding.agent;
	// normalizeKind maps them onto the flat gateway kinds at dispatch time.
	// Kept for legacy data only — new agents write the flat kinds directly.
	KindManaged Kind = "managed"
	// KindHermes is the Hermes Agent gateway client.
	KindHermes Kind = "hermes"
	// KindOpenClaw is the OpenClaw Gateway client.
	KindOpenClaw Kind = "openclaw"
	// KindDeepSeek is the DeepSeek harness (dsh web) gateway client.
	// Declared here so validation/state code has the constant; the
	// dispatch case lands with DeepSeekClient.
	KindDeepSeek Kind = "deepseek"
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
	// Disabled toggles the OpenClaw harness off. When true the factory
	// always returns the ManagedClient stub and the platform advertises
	// the harness as unavailable to the console UI. Defaults to false
	// (enabled) for backward compatibility with existing deployments.
	Disabled bool
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
	// Disabled toggles the Hermes harness off. When true the factory
	// always returns the ManagedClient stub and the platform advertises
	// the harness as unavailable to the console UI. Defaults to false
	// (enabled) for backward compatibility with existing deployments.
	Disabled bool
}

// Registry resolves a harness.Client per agent based on the agent's
// `_oma.harness` metadata. One Registry is constructed at process start
// and threaded through every session.Machine.
type Registry struct {
	defaultClient  Client
	hermesClient   Client
	openclawClient Client
	deepseekClient Client
	fakeClient     Client
	forceClient    Client // if non-nil, returned for every agent (env-var override)
}

// RegistryConfig holds the per-kind clients. The gateway kinds are
// stateless HTTP clients constructed once at process start (the former
// ManagedFactory indirection existed for the never-implemented daemon
// pool and was removed by the 2026-08 flattening).
type RegistryConfig struct {
	// Default is the client returned for KindDefaultLoop (and for agents
	// with no _oma.harness set, since "" normalizes to default-loop).
	Default Client
	// Hermes is the client for KindHermes. Nil falls back to the
	// ManagedClient stub.
	Hermes Client
	// OpenClaw is the client for KindOpenClaw. Nil falls back to the
	// ManagedClient stub.
	OpenClaw Client
	// DeepSeek is the client for KindDeepSeek. Nil falls back to the
	// ManagedClient stub.
	DeepSeek Client
	// Fake is the client returned for KindFake. Defaults to &FakeClient{}
	// when nil.
	Fake Client
	// Force overrides all dispatch when set. Used by OMA_FAKE_HARNESS to
	// keep the legacy "every session uses this test client" behavior.
	Force Client
}

// NewRegistry builds a Registry from cfg. Gateway kinds that are nil
// fall back to the ManagedClient stub in ClientFor, preserving
// pre-flattening fallback behavior.
func NewRegistry(cfg RegistryConfig) *Registry {
	fake := cfg.Fake
	if fake == nil {
		fake = &FakeClient{}
	}
	return &Registry{
		defaultClient:  cfg.Default,
		hermesClient:   cfg.Hermes,
		openclawClient: cfg.OpenClaw,
		deepseekClient: cfg.DeepSeek,
		fakeClient:     fake,
		forceClient:    cfg.Force,
	}
}

// DefaultOnly builds a Registry that returns defaultClient for every agent
// regardless of _oma.harness. Use this to migrate existing tests with a
// 1-line change: `Harness: c` → `HarnessRegistry: DefaultOnly(c)`.
// Gateway kinds fall back to the ManagedClient stub.
func DefaultOnly(defaultClient Client) *Registry {
	return &Registry{
		defaultClient: defaultClient,
		fakeClient:    &FakeClient{},
	}
}

// normalizeKind maps an agent's harness fields onto a flat Kind.
// Legacy two-layer agents (harness="managed" + runtime_binding.agent) are
// normalized here at dispatch time, the same pattern as the "pipy" alias
// for default-loop — no data migration needed.
func normalizeKind(agent store.AgentConfig) (Kind, error) {
	kind := Kind(agent.Harness)
	switch kind {
	case "":
		return KindDefaultLoop, nil
	case "pipy":
		return KindDefaultLoop, nil
	case KindDefaultLoop, KindHermes, KindOpenClaw, KindDeepSeek, KindFake:
		return kind, nil
	case KindManaged:
		b, err := ParseManagedBinding(agent.RuntimeBinding)
		if err != nil {
			return "", err
		}
		switch b.Agent {
		case "hermes":
			return KindHermes, nil
		case "openclaw":
			return KindOpenClaw, nil
		default:
			// claude-acp / codex-acp / anything else keeps the
			// pre-flattening pass-through to the OpenClaw client
			// (model "openclaw/<agent>" — see OpenclawModel).
			return KindOpenClaw, nil
		}
	}
	return "", fmt.Errorf("harness registry: unknown kind %q", kind)
}

// OpenclawModel maps a legacy runtime_binding.agent value to the
// OpenClaw model id. Pre-flattening behavior: "openclaw" becomes
// "openclaw/default", anything else becomes "openclaw/<agent>".
// Exported so cmd/oma-server can assemble the single client.
func OpenclawModel(bindingAgent string) string {
	if bindingAgent == "openclaw" {
		return "openclaw/default"
	}
	return "openclaw/" + bindingAgent
}

// ClientFor resolves the harness.Client for the given agent config.
func (r *Registry) ClientFor(agent store.AgentConfig) (Client, error) {
	if r.forceClient != nil {
		return r.forceClient, nil
	}
	kind, err := normalizeKind(agent)
	if err != nil {
		return nil, err
	}
	switch kind {
	case KindDefaultLoop:
		if r.defaultClient == nil {
			return nil, fmt.Errorf(
				"harness registry: default-loop client not configured")
		}
		return r.defaultClient, nil
	case KindHermes:
		if r.hermesClient == nil {
			return ManagedClient{}, nil
		}
		return r.hermesClient, nil
	case KindOpenClaw:
		if r.openclawClient == nil {
			return ManagedClient{}, nil
		}
		return r.openclawClient, nil
	case KindDeepSeek:
		if r.deepseekClient == nil {
			return ManagedClient{}, nil
		}
		return r.deepseekClient, nil
	case KindFake:
		return r.fakeClient, nil
	}
	return nil, fmt.Errorf("harness registry: unknown kind %q", kind)
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

// HarnessState describes which gateway harnesses are currently enabled.
// Surfaced via the /v1/config/harnesses endpoint so the console UI can
// grey out disabled options in the Harness dropdown.
type HarnessState struct {
	OpenClaw bool `json:"openclaw"`
	Hermes   bool `json:"hermes"`
	DeepSeek bool `json:"deepseek"`
}

// HarnessAvailability returns the on/off state of each gateway harness
// based on the configs used to build the clients. A harness is considered
// enabled when it is not Disabled AND its GatewayURL is configured.
func HarnessAvailability(
	oc OpenClawConfig,
	hc HermesConfig,
	ds DeepSeekConfig,
) HarnessState {
	return HarnessState{
		OpenClaw: !oc.Disabled && oc.GatewayURL != "",
		Hermes:   !hc.Disabled && hc.GatewayURL != "",
		DeepSeek: !ds.Disabled && ds.GatewayURL != "",
	}
}

// DeepSeekConfig holds the configuration for the DeepSeek harness (dsh
// web) gateway integration. When GatewayURL is empty or Disabled the
// registry falls back to the ManagedClient stub and the console greys
// out the DeepSeek option.
type DeepSeekConfig struct {
	// GatewayURL is the dsh web gateway base URL, e.g.
	// "http://dsh:3080". No trailing slash.
	GatewayURL string
	// Token is the optional bearer token. dsh web ships without auth;
	// the field exists for future upstream support and reverse proxies.
	Token string
	// Disabled toggles the DeepSeek harness off.
	Disabled bool
}
