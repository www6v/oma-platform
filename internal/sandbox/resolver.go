package sandbox

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
)

// EnvironmentView is a sandbox-agnostic projection of an environment record.
// Defined here (not in store) so the sandbox package does not import store.
// Machine maps store.EnvironmentConfig → EnvironmentView before calling
// Resolver.Resolve.
type EnvironmentView struct {
	ID         string
	ConfigJSON []byte // raw JSON from EnvironmentConfig.Config
}

// ResolveResult carries metadata about how the config was resolved.
// UsedFallback=true means the environment config was absent, invalid, or
// requested an unsupported provider — the resolver returned the global
// config (possibly with modifications). Reason is a short machine-readable
// tag for logging / session events; empty when UsedFallback is false.
type ResolveResult struct {
	UsedFallback bool
	Reason       string
}

// EnvironmentSandboxConfig is the schema of Environment.config JSON.
//
// Examples:
//
//	{"type": "local"}
//	{"type": "sandbox", "sandbox": {"provider": "opensandbox",
//	  "opensandbox": {"image": "python:3.12-slim", "cpu": "1000m"}}}
type EnvironmentSandboxConfig struct {
	Type    string              `json:"type"`
	Sandbox *SandboxRuntimeSpec `json:"sandbox,omitempty"`
}

// SandboxRuntimeSpec selects a provider and carries provider-specific config.
// Only providers wired up for per-environment binding are honored; others
// fall back to the global config.
type SandboxRuntimeSpec struct {
	Provider    string              `json:"provider"`
	OpenSandbox *OpenSandboxEnvSpec `json:"opensandbox,omitempty"`
}

// OpenSandboxEnvSpec is the per-environment OpenSandbox override. Missing
// fields inherit from the global Config (so deployment-level settings like
// Domain and API key only need to be set once).
type OpenSandboxEnvSpec struct {
	Domain         string `json:"domain,omitempty"`
	Protocol       string `json:"protocol,omitempty"`
	APIKeyEnv      string `json:"api_key_env,omitempty"`
	UseServerProxy *bool  `json:"use_server_proxy,omitempty"`
	ExecdPort      int    `json:"execd_port,omitempty"`
	Image          string `json:"image,omitempty"`
	Entrypoint     string `json:"entrypoint,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
	CPU            string `json:"cpu,omitempty"`
	Memory         string `json:"memory,omitempty"`
}

// Resolver builds per-session sandbox configs by merging an environment
// view with the global Config.
//
// Invariants:
//   - Resolve never returns an error. On any problem (missing env, invalid
//     JSON, unknown type, unsupported provider) it returns the global cfg
//     with UsedFallback=true and a Reason tag.
//   - The returned Config is always usable — callers can pass it straight
//     to Registry.AcquireWith. Validation errors (e.g. missing domain) are
//     surfaced later by Executor construction, matching today's behaviour.
type Resolver struct {
	globalCfg Config
}

// NewResolver returns a resolver backed by the given global config.
func NewResolver(globalCfg Config) *Resolver {
	return &Resolver{globalCfg: globalCfg}
}

// Global returns the underlying global config (used as fallback source).
func (r *Resolver) Global() Config {
	return r.globalCfg
}

// Resolve merges envView into the global config. envView may be nil.
func (r *Resolver) Resolve(env *EnvironmentView) (Config, ResolveResult) {
	if env == nil || len(env.ConfigJSON) == 0 {
		return r.globalCfg, ResolveResult{
			UsedFallback: true,
			Reason:       "no_environment_config",
		}
	}

	var parsed EnvironmentSandboxConfig
	if err := json.Unmarshal(env.ConfigJSON, &parsed); err != nil {
		return r.globalCfg, ResolveResult{
			UsedFallback: true,
			Reason:       "invalid_json",
		}
	}

	switch parsed.Type {
	case "", "local":
		// Legacy environments (no type) and explicit local both mean
		// "use the host workdir, not a sandbox". This is NOT a fallback
		// — it's a deliberate choice recorded in env.config.
		return Config{Provider: ProviderLocal}, ResolveResult{}
	case "sandbox":
		return r.resolveSandbox(parsed.Sandbox)
	default:
		return r.globalCfg, ResolveResult{
			UsedFallback: true,
			Reason:       "unknown_type:" + parsed.Type,
		}
	}
}

func (r *Resolver) resolveSandbox(
	spec *SandboxRuntimeSpec,
) (Config, ResolveResult) {
	if spec == nil || spec.Provider == "" {
		return r.globalCfg, ResolveResult{
			UsedFallback: true,
			Reason:       "sandbox_spec_missing",
		}
	}

	switch spec.Provider {
	case ProviderLocal:
		return Config{Provider: ProviderLocal}, ResolveResult{}
	case ProviderOpenSandbox:
		return r.resolveOpenSandbox(spec.OpenSandbox), ResolveResult{}
	case ProviderE2B, ProviderDaytona, ProviderLiteBox, ProviderBoxRun:
		// Not yet supported per-environment. When a future iteration
		// wires these up, add cases here that mirror resolveOpenSandbox.
		return r.globalCfg, ResolveResult{
			UsedFallback: true,
			Reason:       "provider_not_yet_per_env:" + spec.Provider,
		}
	default:
		return r.globalCfg, ResolveResult{
			UsedFallback: true,
			Reason:       "unknown_provider:" + spec.Provider,
		}
	}
}

// resolveOpenSandbox starts from the global OpenSandbox config and layers
// env-specific overrides on top. Missing fields inherit from global so
// deployment-level settings (domain, api key) don't need to be repeated.
func (r *Resolver) resolveOpenSandbox(spec *OpenSandboxEnvSpec) Config {
	cfg := r.globalCfg
	cfg.Provider = ProviderOpenSandbox
	if spec == nil {
		return cfg
	}
	if spec.Domain != "" {
		cfg.OpenSandboxDomain = spec.Domain
	}
	if spec.Protocol != "" {
		cfg.OpenSandboxProtocol = spec.Protocol
	}
	if spec.APIKeyEnv != "" {
		// Resolve the referenced env var at resolve-time so key
		// rotation doesn't require a database write. If the env var
		// is unset, the resulting API key is empty (same as passing
		// "" explicitly) — matches LoadConfigFromEnv behaviour.
		cfg.OpenSandboxAPIKey = os.Getenv(spec.APIKeyEnv)
	}
	if spec.UseServerProxy != nil {
		cfg.OpenSandboxUseServerProxy = *spec.UseServerProxy
	}
	if spec.ExecdPort != 0 {
		cfg.OpenSandboxExecdPort = spec.ExecdPort
	}
	if spec.Image != "" {
		cfg.OpenSandboxImage = spec.Image
	}
	if spec.Entrypoint != "" {
		cfg.OpenSandboxEntrypoint = spec.Entrypoint
	}
	if spec.TimeoutSeconds != 0 {
		cfg.OpenSandboxTimeoutSec = spec.TimeoutSeconds
	}
	if spec.CPU != "" {
		cfg.OpenSandboxCPU = spec.CPU
	}
	if spec.Memory != "" {
		cfg.OpenSandboxMemory = spec.Memory
	}
	return cfg
}

// ValidateConfigJSON validates an Environment.config JSON blob for the
// POST/PUT /v1/environments API. The validation is intentionally lenient:
//
//   - Empty / missing config: OK (legacy default)
//   - Unknown `type` value: OK (forward compat; the Resolver will treat it
//     as a fallback at resolve-time)
//   - Unknown `sandbox.provider`: OK (forward compat)
//   - Unknown fields anywhere: OK (forward compat)
//   - Wrong JSON types for known fields (string where int expected, etc.):
//     REJECTED with a descriptive error
//   - Malformed JSON: REJECTED
//
// This catches the most common operator errors (typos in numeric fields,
// wrong nesting) without locking out future schema extensions.
func ValidateConfigJSON(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var cfg EnvironmentSandboxConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return fmt.Errorf(
			"environment config schema: %w",
			compactUnmarshalError(err),
		)
	}
	// Additional semantic checks beyond type-correctness.
	if cfg.Type == "sandbox" && cfg.Sandbox == nil {
		return fmt.Errorf(
			"environment config schema: sandbox is required when type=sandbox",
		)
	}
	if cfg.Sandbox != nil {
		switch cfg.Sandbox.Provider {
		case ProviderOpenSandbox:
			if err := validateOpenSandboxEnvSpec(cfg.Sandbox.OpenSandbox); err != nil {
				return err
			}
		case ProviderLocal, ProviderE2B, ProviderDaytona,
			ProviderLiteBox, ProviderBoxRun:
			// Known providers; no per-provider schema yet.
		case "":
			if cfg.Type == "sandbox" {
				return fmt.Errorf(
					"environment config schema: sandbox.provider is required when type=sandbox",
				)
			}
		default:
			// Unknown provider: tolerated for forward compat.
		}
	}
	return nil
}

// validateOpenSandboxEnvSpec applies OpenSandbox-specific semantic checks
// on top of the type validation already done by json.Unmarshal.
func validateOpenSandboxEnvSpec(spec *OpenSandboxEnvSpec) error {
	if spec == nil {
		return nil
	}
	if spec.ExecdPort < 0 || spec.ExecdPort > 65535 {
		return fmt.Errorf(
			"environment config schema: opensandbox.execd_port out of range (0-65535)",
		)
	}
	if spec.TimeoutSeconds < 0 {
		return fmt.Errorf(
			"environment config schema: opensandbox.timeout_seconds must be non-negative",
		)
	}
	return nil
}

// compactUnmarshalError rewrites json.UnmarshalError messages into a form
// that's more useful in an HTTP error response: "field foo: expected int,
// got string" instead of the default long-winded form.
func compactUnmarshalError(err error) error {
	var ute *json.UnmarshalTypeError
	if errors.As(err, &ute) {
		return fmt.Errorf(
			"field %q: expected %s, got %s",
			ute.Field, ute.Type, ute.Value,
		)
	}
	var se *json.SyntaxError
	if errors.As(err, &se) {
		return fmt.Errorf("invalid json at offset %d", se.Offset)
	}
	return err
}
