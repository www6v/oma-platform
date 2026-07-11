package sandbox

import (
	"fmt"
	"os"
	"strconv"
)

// Provider names match open-managed-agents SANDBOX_PROVIDER values.
const (
	ProviderLocal    = "local"
	ProviderE2B      = "e2b"
	ProviderDaytona  = "daytona"
	ProviderLiteBox  = "litebox"
	ProviderBoxRun   = "boxrun"
)

// Config holds sandbox provider settings from the environment.
type Config struct {
	Provider          string
	E2BAPIKey         string
	E2BTemplateID     string
	E2BAPIBase        string
	DaytonaAPIKey     string
	DaytonaAPIBase    string
	DaytonaProxy      string
	SandboxImage      string
	BoxRunURL         string
	BoxRunToken       string
	BoxRunCPUs        int
	BoxRunMemoryMib   int
	LiteBoxMemoryMib  int
	LiteBoxCPUs       int
}

// LoadConfigFromEnv reads SANDBOX_PROVIDER and provider credentials.
func LoadConfigFromEnv() Config {
	provider := normalizeProviderName(
		envOrDefault("SANDBOX_PROVIDER", ProviderLocal),
	)
	cfg := Config{
		Provider:       provider,
		E2BAPIKey:      os.Getenv("E2B_API_KEY"),
		E2BTemplateID:  envOrDefault("SANDBOX_IMAGE", "base"),
		E2BAPIBase:     envOrDefault("E2B_API_BASE", "https://api.e2b.app"),
		DaytonaAPIKey:  os.Getenv("DAYTONA_API_KEY"),
		DaytonaAPIBase: envOrDefault("DAYTONA_API_URL", "https://app.daytona.io/api"),
		DaytonaProxy: envOrDefault(
			"DAYTONA_TOOLBOX_PROXY",
			"https://proxy.app.daytona.io/toolbox",
		),
		SandboxImage: envOrDefault("SANDBOX_IMAGE", "node:22-slim"),
		BoxRunURL:    os.Getenv("BOXRUN_URL"),
		BoxRunToken:  os.Getenv("BOXRUN_TOKEN"),
	}
	if v := os.Getenv("BOXRUN_CPUS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.BoxRunCPUs = n
		}
	}
	if v := os.Getenv("BOXRUN_MEMORY_MIB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.BoxRunMemoryMib = n
		}
	}
	if v := os.Getenv("LITEBOX_MEMORY_MIB"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.LiteBoxMemoryMib = n
		}
	}
	if v := os.Getenv("LITEBOX_CPUS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.LiteBoxCPUs = n
		}
	}
	return cfg
}

// Validate returns an error when a remote provider is selected without creds.
func (c Config) Validate() error {
	switch c.Provider {
	case ProviderLocal, "":
		return nil
	case ProviderE2B:
		if c.E2BAPIKey == "" {
			return fmt.Errorf("E2B_API_KEY required when SANDBOX_PROVIDER=e2b")
		}
		return nil
	case ProviderDaytona:
		if c.DaytonaAPIKey == "" {
			return fmt.Errorf(
				"DAYTONA_API_KEY required when SANDBOX_PROVIDER=daytona",
			)
		}
		return nil
	case ProviderLiteBox:
		return nil
	case ProviderBoxRun:
		if c.BoxRunURL == "" {
			return fmt.Errorf(
				"BOXRUN_URL required when SANDBOX_PROVIDER=boxrun",
			)
		}
		return nil
	default:
		return fmt.Errorf(
			"SANDBOX_PROVIDER=%q not recognized "+
				"(local, litebox, boxlite, boxrun, e2b, daytona)",
			c.Provider,
		)
	}
}

// IsRemote reports whether bash should run outside the host workdir.
func (c Config) IsRemote() bool {
	switch c.Provider {
	case ProviderE2B, ProviderDaytona, ProviderLiteBox, ProviderBoxRun:
		return true
	default:
		return false
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
