package sandbox

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	e2bEnvdHost       = "https://sandbox.e2b.app"
	e2bEnvdPort       = "49983"
	connectVersion    = "1"
)

// E2BExecutor runs commands in an E2B Firecracker microVM.
type E2BExecutor struct {
	cfg        Config
	httpClient *http.Client
	sandboxID  string
	accessTok  string
}

type e2bCreateRequest struct {
	TemplateID string `json:"templateID"`
	Timeout    int    `json:"timeout,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type e2bCreateResponse struct {
	SandboxID       string  `json:"sandboxID"`
	EnvdAccessToken *string `json:"envdAccessToken"`
}

// NewE2BExecutor creates a remote E2B sandbox.
func NewE2BExecutor(
	ctx context.Context,
	cfg Config,
	sessionID string,
	httpClient *http.Client,
) (*E2BExecutor, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	templateID := cfg.E2BTemplateID
	if templateID == "" {
		templateID = "base"
	}
	body, _ := json.Marshal(e2bCreateRequest{
		TemplateID: templateID,
		Timeout:    3600,
		Metadata:   map[string]string{"oma_session_id": sessionID},
	})
	url := strings.TrimRight(cfg.E2BAPIBase, "/") + "/sandboxes"
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, url, bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", cfg.E2BAPIKey)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf(
			"e2b create status=%d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(raw)),
		)
	}
	var created e2bCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, err
	}
	if created.SandboxID == "" {
		return nil, fmt.Errorf("e2b create: missing sandboxID")
	}
	exec := &E2BExecutor{
		cfg:        cfg,
		httpClient: httpClient,
		sandboxID:  created.SandboxID,
	}
	if created.EnvdAccessToken != nil {
		exec.accessTok = *created.EnvdAccessToken
	}
	return exec, nil
}

// Provider implements Executor.
func (*E2BExecutor) Provider() string {
	return ProviderE2B
}

// Exec implements Executor via E2B envd Connect process API.
func (e *E2BExecutor) Exec(
	ctx context.Context,
	command string,
	timeout time.Duration,
) (string, error) {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	payload, _ := json.Marshal(map[string]any{
		"process": map[string]any{
			"cmd":  "/bin/sh",
			"args": []string{"-c", command},
			"cwd":  "/workspace",
		},
	})
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		e2bEnvdHost+"/process.Process/Start",
		bytes.NewReader(payload),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/connect+json")
	req.Header.Set("Connect-Protocol-Version", connectVersion)
	req.Header.Set("Connect-Timeout-Ms", fmt.Sprintf("%d", timeout.Milliseconds()))
	req.Header.Set("E2b-Sandbox-Id", e.sandboxID)
	req.Header.Set("E2b-Sandbox-Port", e2bEnvdPort)
	if e.accessTok != "" {
		req.Header.Set("X-Access-Token", e.accessTok)
	}
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf(
			"e2b exec status=%d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(raw)),
		)
	}
	return parseE2BConnectOutput(resp.Body)
}

// ReadFile reads via envd filesystem API (base64 shell fallback).
func (e *E2BExecutor) ReadFile(ctx context.Context, path string) ([]byte, error) {
	normalized := path
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/workspace/" + strings.TrimPrefix(normalized, "/")
	}
	out, err := e.Exec(
		ctx,
		fmt.Sprintf("base64 -w0 %q", normalized),
		30*time.Second,
	)
	if err != nil {
		return nil, err
	}
	if strings.Contains(out, "[exit ") {
		return nil, fmt.Errorf("e2b read failed: %s", out)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(out))
	if err != nil {
		return nil, err
	}
	return decoded, nil
}

// Destroy kills the E2B sandbox.
func (e *E2BExecutor) Destroy(ctx context.Context) error {
	if e.sandboxID == "" {
		return nil
	}
	url := fmt.Sprintf(
		"%s/sandboxes/%s",
		strings.TrimRight(e.cfg.E2BAPIBase, "/"),
		e.sandboxID,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", e.cfg.E2BAPIKey)
	resp, err := e.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotFound {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf(
			"e2b delete status=%d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(raw)),
		)
	}
	e.sandboxID = ""
	return nil
}

func parseE2BConnectOutput(r io.Reader) (string, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return "", err
	}
	var stdout, stderr strings.Builder
	exitCode := 0
	lines := bytes.Split(raw, []byte("\n"))
	for _, line := range lines {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var msg struct {
			Event struct {
				Data *struct {
					Stdout *string `json:"stdout"`
					Stderr *string `json:"stderr"`
				} `json:"data"`
				End *struct {
					ExitCode *int   `json:"exitCode"`
					Status   string `json:"status"`
				} `json:"end"`
			} `json:"event"`
		}
		if err := json.Unmarshal(line, &msg); err != nil {
			continue
		}
		if msg.Event.Data != nil {
			if msg.Event.Data.Stdout != nil {
				b, _ := base64.StdEncoding.DecodeString(*msg.Event.Data.Stdout)
				stdout.Write(b)
			}
			if msg.Event.Data.Stderr != nil {
				b, _ := base64.StdEncoding.DecodeString(*msg.Event.Data.Stderr)
				stderr.Write(b)
			}
		}
		if msg.Event.End != nil {
			if msg.Event.End.ExitCode != nil {
				exitCode = *msg.Event.End.ExitCode
			} else if strings.Contains(msg.Event.End.Status, " ") {
				var code int
				_, _ = fmt.Sscanf(msg.Event.End.Status, "exit status %d", &code)
				exitCode = code
			}
		}
	}
	combined := strings.TrimRight(stdout.String()+stderr.String(), "\n")
	if exitCode != 0 {
		return combined + fmt.Sprintf("\n[exit %d]", exitCode), nil
	}
	return combined, nil
}
