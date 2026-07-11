package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DaytonaExecutor runs commands in a Daytona sandbox VM.
type DaytonaExecutor struct {
	cfg        Config
	httpClient *http.Client
	sandboxID  string
}

type daytonaCreateResponse struct {
	ID string `json:"id"`
}

type daytonaExecRequest struct {
	Command string `json:"command"`
	Cwd     string `json:"cwd,omitempty"`
	Timeout int    `json:"timeout,omitempty"`
}

type daytonaExecResponse struct {
	Result   string `json:"result"`
	ExitCode int    `json:"exitCode"`
}

// NewDaytonaExecutor creates a remote Daytona sandbox.
func NewDaytonaExecutor(
	ctx context.Context,
	cfg Config,
	httpClient *http.Client,
) (*DaytonaExecutor, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	body, _ := json.Marshal(map[string]any{
		"image": cfg.SandboxImage,
	})
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		strings.TrimRight(cfg.DaytonaAPIBase, "/")+"/sandbox",
		bytes.NewReader(body),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cfg.DaytonaAPIKey)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf(
			"daytona create status=%d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(raw)),
		)
	}
	var created daytonaCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, err
	}
	if created.ID == "" {
		return nil, fmt.Errorf("daytona create: missing sandbox id")
	}
	return &DaytonaExecutor{
		cfg:        cfg,
		httpClient: httpClient,
		sandboxID:  created.ID,
	}, nil
}

// Provider implements Executor.
func (*DaytonaExecutor) Provider() string {
	return ProviderDaytona
}

// Exec implements Executor.
func (d *DaytonaExecutor) Exec(
	ctx context.Context,
	command string,
	timeout time.Duration,
) (string, error) {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	payload, _ := json.Marshal(daytonaExecRequest{
		Command: command,
		Cwd:     "/workspace",
		Timeout: int(timeout.Seconds()),
	})
	url := fmt.Sprintf(
		"%s/%s/process/execute",
		strings.TrimRight(d.cfg.DaytonaProxy, "/"),
		d.sandboxID,
	)
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, url, bytes.NewReader(payload),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+d.cfg.DaytonaAPIKey)
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf(
			"daytona exec status=%d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(raw)),
		)
	}
	var out daytonaExecResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	combined := strings.TrimRight(out.Result, "\n")
	if out.ExitCode != 0 {
		return combined + fmt.Sprintf("\n[exit %d]", out.ExitCode), nil
	}
	return combined, nil
}

// ReadFile reads a file via toolbox download (best-effort).
func (d *DaytonaExecutor) ReadFile(
	ctx context.Context,
	path string,
) ([]byte, error) {
	normalized := path
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/workspace/" + strings.TrimPrefix(normalized, "/")
	}
	url := fmt.Sprintf(
		"%s/%s/files/download?path=%s",
		strings.TrimRight(d.cfg.DaytonaProxy, "/"),
		d.sandboxID,
		normalized,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+d.cfg.DaytonaAPIKey)
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf(
			"daytona read status=%d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(raw)),
		)
	}
	return io.ReadAll(resp.Body)
}

// Destroy deletes the remote sandbox.
func (d *DaytonaExecutor) Destroy(ctx context.Context) error {
	if d.sandboxID == "" {
		return nil
	}
	url := fmt.Sprintf(
		"%s/sandbox/%s",
		strings.TrimRight(d.cfg.DaytonaAPIBase, "/"),
		d.sandboxID,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+d.cfg.DaytonaAPIKey)
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotFound {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf(
			"daytona delete status=%d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(raw)),
		)
	}
	d.sandboxID = ""
	return nil
}
