package sandbox

import (
	"fmt"
	"os"
	"strconv"
)

// Provider names match open-managed-agents SANDBOX_PROVIDER values.
const (
	ProviderLocal       = "local"
	ProviderE2B         = "e2b"
	ProviderDaytona     = "daytona"
	ProviderLiteBox     = "litebox"
	ProviderBoxRun      = "boxrun"
	ProviderOpenSandbox = "opensandbox"
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

	// OpenSandbox (two-layer API: Lifecycle Server + execd via server proxy).
	OpenSandboxDomain        string // e.g. "124.221.28.203:18090"
	OpenSandboxProtocol      string // "http" or "https"; default "http"
	OpenSandboxAPIKey        string // optional; empty => INSECURE mode
	OpenSandboxUseServerProxy bool  // default true; execd traffic via server proxy
	OpenSandboxExecdPort     int    // default 44772
	OpenSandboxImage         string // default "python:3.12"
	OpenSandboxEntrypoint    string // optional; defaults to image entrypoint
	OpenSandboxTimeoutSec    int    // default 3600
	OpenSandboxCPU           string // default "500m"
	OpenSandboxMemory        string // default "512Mi"
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

		OpenSandboxDomain:         os.Getenv("OPENSANDBOX_DOMAIN"),
		OpenSandboxProtocol:       envOrDefault("OPENSANDBOX_PROTOCOL", "http"),
		OpenSandboxAPIKey:         os.Getenv("OPENSANDBOX_API_KEY"),
		OpenSandboxUseServerProxy: envBool("OPENSANDBOX_USE_SERVER_PROXY", true),
		OpenSandboxExecdPort:      envInt("OPENSANDBOX_EXECD_PORT", 44772),
		OpenSandboxImage:          envOrDefault("OPENSANDBOX_IMAGE", "python:3.12-slim"),
		OpenSandboxEntrypoint:     os.Getenv("OPENSANDBOX_ENTRYPOINT"),
		OpenSandboxTimeoutSec:     envInt("OPENSANDBOX_TIMEOUT_SECONDS", 3600),
		OpenSandboxCPU:            envOrDefault("OPENSANDBOX_CPU", "500m"),
		OpenSandboxMemory:         envOrDefault("OPENSANDBOX_MEMORY", "512Mi"),
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
	case ProviderOpenSandbox:
		if c.OpenSandboxDomain == "" {
			return fmt.Errorf(
				"OPENSANDBOX_DOMAIN required when SANDBOX_PROVIDER=opensandbox",
			)
		}
		return nil
	default:
		return fmt.Errorf(
			"SANDBOX_PROVIDER=%q not recognized "+
				"(local, litebox, boxlite, boxrun, e2b, daytona, opensandbox)",
			c.Provider,
		)
	}
}

// IsRemote reports whether bash should run outside the host workdir.
func (c Config) IsRemote() bool {
	switch c.Provider {
	case ProviderE2B, ProviderDaytona, ProviderLiteBox, ProviderBoxRun,
		ProviderOpenSandbox:
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

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func envBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	switch v {
	case "1", "true", "TRUE", "True", "yes", "YES", "on", "ON":
		return true
	case "0", "false", "FALSE", "False", "no", "NO", "off", "OFF":
		return false
	}
	return fallback
}
