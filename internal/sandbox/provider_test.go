package sandbox_test

import (
	"testing"

	"github.com/open-ma/oma-building/internal/sandbox"
)

func TestLoadConfigFromEnvDefaultsLocal(t *testing.T) {
	t.Setenv("SANDBOX_PROVIDER", "")
	t.Setenv("E2B_API_KEY", "")
	cfg := sandbox.LoadConfigFromEnv()
	if cfg.Provider != sandbox.ProviderLocal {
		t.Fatalf("provider=%q want local", cfg.Provider)
	}
	if cfg.IsRemote() {
		t.Fatal("local provider should not be remote")
	}
}

func TestValidateRemoteProviders(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		provider string
		key     string
		wantErr bool
	}{
		{"e2b missing key", sandbox.ProviderE2B, "", true},
		{"e2b ok", sandbox.ProviderE2B, "key", false},
		{"daytona missing key", sandbox.ProviderDaytona, "", true},
		{"daytona ok", sandbox.ProviderDaytona, "key", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := sandbox.Config{Provider: tc.provider}
			if tc.provider == sandbox.ProviderE2B {
				cfg.E2BAPIKey = tc.key
			}
			if tc.provider == sandbox.ProviderDaytona {
				cfg.DaytonaAPIKey = tc.key
			}
			err := cfg.Validate()
			if tc.wantErr && err == nil {
				t.Fatal("expected error")
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}

func TestIsRemote(t *testing.T) {
	t.Parallel()
	for _, p := range []string{
		sandbox.ProviderE2B,
		sandbox.ProviderDaytona,
		sandbox.ProviderLiteBox,
		sandbox.ProviderBoxRun,
		sandbox.ProviderOpenSandbox,
	} {
		cfg := sandbox.Config{Provider: p}
		if !cfg.IsRemote() {
			t.Fatalf("%s should be remote", p)
		}
	}
}
