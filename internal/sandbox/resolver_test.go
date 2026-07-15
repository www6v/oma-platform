package sandbox

import (
	"strings"
	"testing"
)

func TestResolver_NilEnvView_ReturnsGlobalWithFallback(t *testing.T) {
	g := Config{
		Provider:          ProviderOpenSandbox,
		OpenSandboxDomain: "g.example.com:18090",
	}
	r := NewResolver(g)

	cfg, res := r.Resolve(nil)
	if cfg.Provider != ProviderOpenSandbox {
		t.Fatalf("provider=%q", cfg.Provider)
	}
	if cfg.OpenSandboxDomain != "g.example.com:18090" {
		t.Fatalf("domain not inherited: %q", cfg.OpenSandboxDomain)
	}
	if !res.UsedFallback || res.Reason != "no_environment_config" {
		t.Fatalf("expected fallback, got %+v", res)
	}
}

func TestResolver_EmptyConfigJSON_ReturnsGlobalWithFallback(t *testing.T) {
	g := Config{Provider: ProviderOpenSandbox, OpenSandboxDomain: "g.example.com:18090"}
	r := NewResolver(g)

	cfg, res := r.Resolve(&EnvironmentView{ID: "env-1"})
	if cfg.Provider != ProviderOpenSandbox {
		t.Fatalf("provider=%q", cfg.Provider)
	}
	if !res.UsedFallback {
		t.Fatalf("expected fallback for empty config")
	}
}

func TestResolver_EmptyJSONObject_TreatedAsNoType_Local(t *testing.T) {
	// {} has no "type" field — switch treats "" as local (legacy compat).
	g := Config{Provider: ProviderOpenSandbox, OpenSandboxDomain: "g.example.com:18090"}
	r := NewResolver(g)

	cfg, res := r.Resolve(&EnvironmentView{
		ID: "env-1", ConfigJSON: []byte(`{}`),
	})
	if cfg.Provider != ProviderLocal {
		t.Fatalf("expected local for empty object, got %q", cfg.Provider)
	}
	if res.UsedFallback {
		t.Fatalf("legacy empty-object should not be a fallback")
	}
}

func TestResolver_TypeLocal_ReturnsLocalNoFallback(t *testing.T) {
	g := Config{Provider: ProviderOpenSandbox, OpenSandboxDomain: "g.example.com:18090"}
	r := NewResolver(g)

	cfg, res := r.Resolve(&EnvironmentView{
		ID: "env-default", ConfigJSON: []byte(`{"type":"local"}`),
	})
	if cfg.Provider != ProviderLocal {
		t.Fatalf("expected local provider, got %q", cfg.Provider)
	}
	if res.UsedFallback {
		t.Fatalf("type=local should not be a fallback")
	}
}

func TestResolver_InvalidJSON_ReturnsGlobalWithFallback(t *testing.T) {
	g := Config{Provider: ProviderLocal}
	r := NewResolver(g)

	cfg, res := r.Resolve(&EnvironmentView{
		ID: "env-1", ConfigJSON: []byte(`not json`),
	})
	if cfg.Provider != ProviderLocal {
		t.Fatalf("expected global provider, got %q", cfg.Provider)
	}
	if !res.UsedFallback || res.Reason != "invalid_json" {
		t.Fatalf("expected invalid_json fallback, got %+v", res)
	}
}

func TestResolver_OpenSandbox_InheritsAllFieldsFromGlobal(t *testing.T) {
	g := Config{
		Provider:                  ProviderOpenSandbox,
		OpenSandboxDomain:         "g.example.com:18090",
		OpenSandboxProtocol:       "https",
		OpenSandboxAPIKey:         "global-key",
		OpenSandboxUseServerProxy: true,
		OpenSandboxExecdPort:      44772,
		OpenSandboxImage:          "python:3.12-slim",
		OpenSandboxTimeoutSec:     3600,
		OpenSandboxCPU:            "500m",
		OpenSandboxMemory:         "512Mi",
	}
	r := NewResolver(g)

	envJSON := `{"type":"sandbox","sandbox":{"provider":"opensandbox"}}`
	cfg, res := r.Resolve(&EnvironmentView{ID: "env-1", ConfigJSON: []byte(envJSON)})

	if res.UsedFallback {
		t.Fatalf("opensandbox with global backing should not be fallback: %+v", res)
	}
	if cfg.Provider != ProviderOpenSandbox {
		t.Fatalf("provider=%q", cfg.Provider)
	}
	if cfg.OpenSandboxDomain != "g.example.com:18090" {
		t.Fatalf("domain not inherited: %q", cfg.OpenSandboxDomain)
	}
	if cfg.OpenSandboxProtocol != "https" {
		t.Fatalf("protocol not inherited: %q", cfg.OpenSandboxProtocol)
	}
	if cfg.OpenSandboxAPIKey != "global-key" {
		t.Fatalf("api key not inherited: %q", cfg.OpenSandboxAPIKey)
	}
	if !cfg.OpenSandboxUseServerProxy {
		t.Fatalf("use_server_proxy not inherited")
	}
	if cfg.OpenSandboxExecdPort != 44772 {
		t.Fatalf("execd_port not inherited: %d", cfg.OpenSandboxExecdPort)
	}
	if cfg.OpenSandboxImage != "python:3.12-slim" {
		t.Fatalf("image not inherited: %q", cfg.OpenSandboxImage)
	}
	if cfg.OpenSandboxTimeoutSec != 3600 {
		t.Fatalf("timeout not inherited: %d", cfg.OpenSandboxTimeoutSec)
	}
	if cfg.OpenSandboxCPU != "500m" {
		t.Fatalf("cpu not inherited: %q", cfg.OpenSandboxCPU)
	}
	if cfg.OpenSandboxMemory != "512Mi" {
		t.Fatalf("memory not inherited: %q", cfg.OpenSandboxMemory)
	}
}

func TestResolver_OpenSandbox_OverridesSelectedFields(t *testing.T) {
	g := Config{
		Provider:                  ProviderOpenSandbox,
		OpenSandboxDomain:         "g.example.com:18090",
		OpenSandboxImage:          "python:3.12-slim",
		OpenSandboxUseServerProxy: true,
		OpenSandboxCPU:            "500m",
		OpenSandboxMemory:         "512Mi",
	}
	r := NewResolver(g)

	envJSON := `{
		"type":"sandbox",
		"sandbox":{
			"provider":"opensandbox",
			"opensandbox":{
				"image":"custom:v1",
				"cpu":"1000m",
				"domain":"other.example.com:18090"
			}
		}
	}`
	cfg, res := r.Resolve(&EnvironmentView{ID: "env-1", ConfigJSON: []byte(envJSON)})
	if res.UsedFallback {
		t.Fatalf("should not be fallback: %+v", res)
	}
	if cfg.OpenSandboxImage != "custom:v1" {
		t.Fatalf("image override failed: %q", cfg.OpenSandboxImage)
	}
	if cfg.OpenSandboxCPU != "1000m" {
		t.Fatalf("cpu override failed: %q", cfg.OpenSandboxCPU)
	}
	if cfg.OpenSandboxDomain != "other.example.com:18090" {
		t.Fatalf("domain override failed: %q", cfg.OpenSandboxDomain)
	}
	// Unspecified fields still inherit from global.
	if cfg.OpenSandboxMemory != "512Mi" {
		t.Fatalf("memory should inherit: %q", cfg.OpenSandboxMemory)
	}
	if !cfg.OpenSandboxUseServerProxy {
		t.Fatalf("use_server_proxy should inherit as true")
	}
}

func TestResolver_OpenSandbox_APIKeyEnvResolution(t *testing.T) {
	t.Setenv("TEST_OPENSANDBOX_KEY", "from-env-var")

	g := Config{
		Provider:          ProviderOpenSandbox,
		OpenSandboxDomain: "g.example.com:18090",
		OpenSandboxAPIKey: "global-key",
	}
	r := NewResolver(g)

	envJSON := `{
		"type":"sandbox",
		"sandbox":{
			"provider":"opensandbox",
			"opensandbox":{"api_key_env":"TEST_OPENSANDBOX_KEY"}
		}
	}`
	cfg, res := r.Resolve(&EnvironmentView{ID: "env-1", ConfigJSON: []byte(envJSON)})
	if res.UsedFallback {
		t.Fatalf("should not be fallback: %+v", res)
	}
	if cfg.OpenSandboxAPIKey != "from-env-var" {
		t.Fatalf("api_key_env not resolved: got %q, want %q",
			cfg.OpenSandboxAPIKey, "from-env-var")
	}
}

func TestResolver_OpenSandbox_APIKeyEnvUnset_EmptyString(t *testing.T) {
	// When the referenced env var is unset, the resolved key should be
	// empty — same as passing "" explicitly. Do NOT fall back to global.
	t.Setenv("DOES_NOT_EXIST_FOR_TEST", "")

	g := Config{
		Provider:          ProviderOpenSandbox,
		OpenSandboxDomain: "g.example.com:18090",
		OpenSandboxAPIKey: "global-key",
	}
	r := NewResolver(g)

	envJSON := `{
		"type":"sandbox",
		"sandbox":{
			"provider":"opensandbox",
			"opensandbox":{"api_key_env":"DOES_NOT_EXIST_FOR_TEST"}
		}
	}`
	cfg, res := r.Resolve(&EnvironmentView{ID: "env-1", ConfigJSON: []byte(envJSON)})
	if res.UsedFallback {
		t.Fatalf("should not be fallback: %+v", res)
	}
	if cfg.OpenSandboxAPIKey != "" {
		t.Fatalf("unset env var should yield empty key, got %q", cfg.OpenSandboxAPIKey)
	}
}

func TestResolver_UnsupportedProvider_ReturnsGlobalFallback(t *testing.T) {
	for _, provider := range []string{"e2b", "daytona", "litebox", "boxrun"} {
		t.Run(provider, func(t *testing.T) {
			g := Config{
				Provider:          ProviderOpenSandbox,
				OpenSandboxDomain: "g.example.com:18090",
			}
			r := NewResolver(g)

			envJSON := `{"type":"sandbox","sandbox":{"provider":"` + provider + `"}}`
			cfg, res := r.Resolve(&EnvironmentView{
				ID: "env-1", ConfigJSON: []byte(envJSON),
			})
			if cfg.Provider != ProviderOpenSandbox {
				t.Fatalf("expected global provider, got %q", cfg.Provider)
			}
			if !res.UsedFallback {
				t.Fatalf("%s should trigger fallback", provider)
			}
			want := "provider_not_yet_per_env:" + provider
			if res.Reason != want {
				t.Fatalf("reason=%q, want %q", res.Reason, want)
			}
		})
	}
}

func TestResolver_UnknownType_ReturnsGlobalFallback(t *testing.T) {
	g := Config{Provider: ProviderLocal}
	r := NewResolver(g)

	envJSON := `{"type":"kubernetes"}`
	cfg, res := r.Resolve(&EnvironmentView{
		ID: "env-1", ConfigJSON: []byte(envJSON),
	})
	if cfg.Provider != ProviderLocal {
		t.Fatalf("expected global, got %q", cfg.Provider)
	}
	if !res.UsedFallback {
		t.Fatalf("unknown type should fallback")
	}
	if !strings.HasPrefix(res.Reason, "unknown_type:") {
		t.Fatalf("reason=%q", res.Reason)
	}
}

func TestResolver_UnknownProvider_ReturnsGlobalFallback(t *testing.T) {
	g := Config{Provider: ProviderLocal}
	r := NewResolver(g)

	envJSON := `{"type":"sandbox","sandbox":{"provider":"firecracker"}}`
	cfg, res := r.Resolve(&EnvironmentView{
		ID: "env-1", ConfigJSON: []byte(envJSON),
	})
	if cfg.Provider != ProviderLocal {
		t.Fatalf("expected global, got %q", cfg.Provider)
	}
	if !res.UsedFallback {
		t.Fatalf("unknown provider should fallback")
	}
	if !strings.HasPrefix(res.Reason, "unknown_provider:") {
		t.Fatalf("reason=%q", res.Reason)
	}
}

func TestResolver_DefaultEnvironment_AlwaysLocalEvenWhenGlobalIsRemote(t *testing.T) {
	// The default environment has config {"type":"local"}; it must stay
	// local even when the deployment's global cfg points at a remote
	// provider. Otherwise SANDBOX_PROVIDER=opensandbox would accidentally
	// sandbox the default environment's sessions.
	g := Config{Provider: ProviderOpenSandbox, OpenSandboxDomain: "g.example.com:18090"}
	r := NewResolver(g)

	cfg, res := r.Resolve(&EnvironmentView{
		ID: "env-local-default", ConfigJSON: []byte(`{"type":"local"}`),
	})
	if cfg.Provider != ProviderLocal {
		t.Fatalf("default env must be local even with remote global: %q", cfg.Provider)
	}
	if res.UsedFallback {
		t.Fatalf("explicit local should not be a fallback")
	}
}

func TestResolver_UseServerProxy_FalseOverrideRespected(t *testing.T) {
	// *bool override: false must not be confused with "missing". This is
	// why the schema uses *bool instead of bool for UseServerProxy.
	g := Config{
		Provider:                  ProviderOpenSandbox,
		OpenSandboxDomain:         "g.example.com:18090",
		OpenSandboxUseServerProxy: true,
	}
	r := NewResolver(g)

	envJSON := `{
		"type":"sandbox",
		"sandbox":{
			"provider":"opensandbox",
			"opensandbox":{"use_server_proxy":false}
		}
	}`
	cfg, _ := r.Resolve(&EnvironmentView{ID: "env-1", ConfigJSON: []byte(envJSON)})
	if cfg.OpenSandboxUseServerProxy {
		t.Fatalf("use_server_proxy=false not respected")
	}
}

func TestResolver_SandboxSpecMissing_ProviderEmpty(t *testing.T) {
	g := Config{Provider: ProviderLocal}
	r := NewResolver(g)

	envJSON := `{"type":"sandbox","sandbox":{}}`
	cfg, res := r.Resolve(&EnvironmentView{ID: "env-1", ConfigJSON: []byte(envJSON)})
	if cfg.Provider != ProviderLocal {
		t.Fatalf("expected global, got %q", cfg.Provider)
	}
	if !res.UsedFallback || res.Reason != "sandbox_spec_missing" {
		t.Fatalf("reason=%+v", res)
	}
}

func TestResolver_SandboxTypeWithoutSpec_Fallback(t *testing.T) {
	g := Config{Provider: ProviderLocal}
	r := NewResolver(g)

	envJSON := `{"type":"sandbox"}`
	cfg, res := r.Resolve(&EnvironmentView{ID: "env-1", ConfigJSON: []byte(envJSON)})
	if cfg.Provider != ProviderLocal {
		t.Fatalf("expected global, got %q", cfg.Provider)
	}
	if !res.UsedFallback || res.Reason != "sandbox_spec_missing" {
		t.Fatalf("reason=%+v", res)
	}
}

func TestResolver_OpenSandbox_AllNumericAndStringOverrides(t *testing.T) {
	g := Config{
		Provider:                ProviderOpenSandbox,
		OpenSandboxDomain:       "g.example.com:18090",
		OpenSandboxProtocol:     "http",
		OpenSandboxExecdPort:    44772,
		OpenSandboxImage:        "python:3.12-slim",
		OpenSandboxEntrypoint:   "",
		OpenSandboxTimeoutSec:   3600,
		OpenSandboxCPU:          "500m",
		OpenSandboxMemory:       "512Mi",
	}
	r := NewResolver(g)

	envJSON := `{
		"type":"sandbox",
		"sandbox":{
			"provider":"opensandbox",
			"opensandbox":{
				"protocol":"https",
				"execd_port":55000,
				"entrypoint":"/bin/sh",
				"timeout_seconds":7200,
				"memory":"1Gi"
			}
		}
	}`
	cfg, _ := r.Resolve(&EnvironmentView{ID: "env-1", ConfigJSON: []byte(envJSON)})
	if cfg.OpenSandboxProtocol != "https" {
		t.Fatalf("protocol override failed: %q", cfg.OpenSandboxProtocol)
	}
	if cfg.OpenSandboxExecdPort != 55000 {
		t.Fatalf("execd_port override failed: %d", cfg.OpenSandboxExecdPort)
	}
	if cfg.OpenSandboxEntrypoint != "/bin/sh" {
		t.Fatalf("entrypoint override failed: %q", cfg.OpenSandboxEntrypoint)
	}
	if cfg.OpenSandboxTimeoutSec != 7200 {
		t.Fatalf("timeout override failed: %d", cfg.OpenSandboxTimeoutSec)
	}
	if cfg.OpenSandboxMemory != "1Gi" {
		t.Fatalf("memory override failed: %q", cfg.OpenSandboxMemory)
	}
	// Untouched fields still inherit.
	if cfg.OpenSandboxDomain != "g.example.com:18090" {
		t.Fatalf("domain should inherit: %q", cfg.OpenSandboxDomain)
	}
	if cfg.OpenSandboxImage != "python:3.12-slim" {
		t.Fatalf("image should inherit: %q", cfg.OpenSandboxImage)
	}
}

func TestResolver_SandboxProviderLocalExplicit(t *testing.T) {
	// {"type":"sandbox","sandbox":{"provider":"local"}} should also yield
	// local provider (some users may express "no sandbox" either way).
	g := Config{Provider: ProviderOpenSandbox, OpenSandboxDomain: "g.example.com:18090"}
	r := NewResolver(g)

	envJSON := `{"type":"sandbox","sandbox":{"provider":"local"}}`
	cfg, res := r.Resolve(&EnvironmentView{ID: "env-1", ConfigJSON: []byte(envJSON)})
	if cfg.Provider != ProviderLocal {
		t.Fatalf("expected local, got %q", cfg.Provider)
	}
	if res.UsedFallback {
		t.Fatalf("explicit local should not be a fallback")
	}
}

// --- ValidateConfigJSON tests ----------------------------------------------

func TestValidateConfigJSON_EmptyAndLegacy(t *testing.T) {
	// Empty / missing config → OK (legacy default environment).
	if err := ValidateConfigJSON(nil); err != nil {
		t.Fatalf("nil: %v", err)
	}
	if err := ValidateConfigJSON([]byte(``)); err != nil {
		t.Fatalf("empty: %v", err)
	}
	// Empty JSON object → OK (legacy type="").
	if err := ValidateConfigJSON([]byte(`{}`)); err != nil {
		t.Fatalf("empty object: %v", err)
	}
	// {"type":"local"} → OK.
	if err := ValidateConfigJSON([]byte(`{"type":"local"}`)); err != nil {
		t.Fatalf("local: %v", err)
	}
}

func TestValidateConfigJSON_RejectsMalformedJSON(t *testing.T) {
	err := ValidateConfigJSON([]byte(`not json`))
	if err == nil {
		t.Fatal("expected error for malformed json")
	}
	if got := err.Error(); !contains(got, "invalid json") {
		t.Fatalf("error should mention invalid json: %q", got)
	}
}

func TestValidateConfigJSON_RejectsWrongTypes(t *testing.T) {
	cases := []struct {
		name      string
		body      string
		wantField string // substring expected in error
	}{
		{
			name:      "type_as_int",
			body:      `{"type": 42}`,
			wantField: "type",
		},
		{
			name:      "sandbox_as_string",
			body:      `{"type":"sandbox","sandbox":"not-object"}`,
			wantField: "sandbox",
		},
		{
			name:      "provider_as_int",
			body:      `{"type":"sandbox","sandbox":{"provider":1}}`,
			wantField: "provider",
		},
		{
			name:      "execd_port_as_string",
			body:      `{"type":"sandbox","sandbox":{"provider":"opensandbox","opensandbox":{"execd_port":"44772"}}}`,
			wantField: "execd_port",
		},
		{
			name:      "use_server_proxy_as_string",
			body:      `{"type":"sandbox","sandbox":{"provider":"opensandbox","opensandbox":{"use_server_proxy":"true"}}}`,
			wantField: "use_server_proxy",
		},
		{
			name:      "timeout_as_string",
			body:      `{"type":"sandbox","sandbox":{"provider":"opensandbox","opensandbox":{"timeout_seconds":"3600"}}}`,
			wantField: "timeout_seconds",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateConfigJSON([]byte(tc.body))
			if err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
			if !contains(err.Error(), tc.wantField) {
				t.Fatalf("error should mention field %q: %v", tc.wantField, err)
			}
		})
	}
}

func TestValidateConfigJSON_AcceptsUnknownTypeForwardCompat(t *testing.T) {
	// Unknown type → OK (Resolver will treat as fallback at runtime, but
	// the API should not reject it; future versions may add new types).
	if err := ValidateConfigJSON([]byte(`{"type":"kubernetes"}`)); err != nil {
		t.Fatalf("unknown type should be tolerated: %v", err)
	}
}

func TestValidateConfigJSON_AcceptsUnknownProviderForwardCompat(t *testing.T) {
	// Unknown provider under type=sandbox → OK (forward compat).
	body := `{"type":"sandbox","sandbox":{"provider":"firecracker","firecracker":{"kernel":"5.10"}}}`
	if err := ValidateConfigJSON([]byte(body)); err != nil {
		t.Fatalf("unknown provider should be tolerated: %v", err)
	}
}

func TestValidateConfigJSON_AcceptsUnknownFieldsForwardCompat(t *testing.T) {
	// Extra fields under opensandbox → OK (forward compat; future fields
	// should not break today's API).
	body := `{
		"type":"sandbox",
		"sandbox":{
			"provider":"opensandbox",
			"opensandbox":{
				"image":"python:3.12-slim",
				"networkPolicy":"allow-all",
				"someFutureField":42
			}
		}
	}`
	if err := ValidateConfigJSON([]byte(body)); err != nil {
		t.Fatalf("unknown fields should be tolerated: %v", err)
	}
}

func TestValidateConfigJSON_ValidOpenSandboxFull(t *testing.T) {
	body := `{
		"type":"sandbox",
		"sandbox":{
			"provider":"opensandbox",
			"opensandbox":{
				"domain":"example.com:18090",
				"protocol":"https",
				"api_key_env":"OPENSANDBOX_API_KEY",
				"use_server_proxy":true,
				"execd_port":44772,
				"image":"python:3.12-slim",
				"entrypoint":"/bin/sh",
				"timeout_seconds":7200,
				"cpu":"1000m",
				"memory":"1Gi"
			}
		}
	}`
	if err := ValidateConfigJSON([]byte(body)); err != nil {
		t.Fatalf("valid opensandbox config rejected: %v", err)
	}
}

func TestValidateConfigJSON_OpensandboxRangeChecks(t *testing.T) {
	// execd_port out of range.
	body := `{"type":"sandbox","sandbox":{"provider":"opensandbox","opensandbox":{"execd_port":70000}}}`
	if err := ValidateConfigJSON([]byte(body)); err == nil {
		t.Fatal("execd_port out of range should error")
	} else if !contains(err.Error(), "execd_port") {
		t.Fatalf("error should mention execd_port: %v", err)
	}
	// Negative timeout.
	body = `{"type":"sandbox","sandbox":{"provider":"opensandbox","opensandbox":{"timeout_seconds":-1}}}`
	if err := ValidateConfigJSON([]byte(body)); err == nil {
		t.Fatal("negative timeout should error")
	} else if !contains(err.Error(), "timeout_seconds") {
		t.Fatalf("error should mention timeout_seconds: %v", err)
	}
}

func TestValidateConfigJSON_SandboxTypeWithoutProvider(t *testing.T) {
	// type=sandbox but provider missing → error.
	body := `{"type":"sandbox","sandbox":{}}`
	err := ValidateConfigJSON([]byte(body))
	if err == nil {
		t.Fatal("type=sandbox without provider should error")
	}
	if !contains(err.Error(), "sandbox.provider") {
		t.Fatalf("error should mention sandbox.provider: %v", err)
	}
}

func TestValidateConfigJSON_SandboxTypeNoSpec(t *testing.T) {
	// type=sandbox, no sandbox field at all → error.
	body := `{"type":"sandbox"}`
	err := ValidateConfigJSON([]byte(body))
	if err == nil {
		t.Fatal("type=sandbox without sandbox field should error")
	}
}

func TestValidateConfigJSON_KnownProvidersOtherThanOpensandbox(t *testing.T) {
	// Known but not-yet-wired providers should be accepted — they're the
	// explicit forward-compat path for Phase 5+.
	for _, p := range []string{"e2b", "daytona", "litebox", "boxrun", "local"} {
		body := `{"type":"sandbox","sandbox":{"provider":"` + p + `"}}`
		if err := ValidateConfigJSON([]byte(body)); err != nil {
			t.Fatalf("provider %q should be accepted: %v", p, err)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

