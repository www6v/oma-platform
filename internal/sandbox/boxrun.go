package sandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// BoxRunExecutor runs commands via BoxRun (boxlite serve) HTTP API.
type BoxRunExecutor struct {
	cfg        Config
	httpClient *http.Client
	sessionID  string
	boxID      string
	boxReady   chan struct{}
	boxErr     error
	envVars    map[string]string
}

// NewBoxRunExecutor creates a BoxRun-backed executor.
func NewBoxRunExecutor(
	ctx context.Context,
	cfg Config,
	sessionID string,
	httpClient *http.Client,
) (*BoxRunExecutor, error) {
	if cfg.BoxRunURL == "" {
		return nil, fmt.Errorf("BOXRUN_URL required when SANDBOX_PROVIDER=boxrun")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	ex := &BoxRunExecutor{
		cfg:        cfg,
		httpClient: httpClient,
		sessionID:  sessionID,
		boxReady:   make(chan struct{}),
		envVars:    make(map[string]string),
	}
	go ex.createBox(ctx)
	return ex, nil
}

// Provider implements Executor.
func (*BoxRunExecutor) Provider() string {
	return ProviderBoxRun
}

// Exec implements Executor.
func (b *BoxRunExecutor) Exec(
	ctx context.Context,
	command string,
	timeout time.Duration,
) (string, error) {
	if err := b.waitBox(ctx); err != nil {
		return "", err
	}
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	body, _ := json.Marshal(map[string]any{
		"command":         "/bin/sh",
		"args":            []string{"-c", command},
		"env":             b.envVars,
		"timeout_seconds": int(timeout.Seconds()),
	})
	startURL := fmt.Sprintf(
		"%s/boxes/%s/exec",
		strings.TrimRight(b.cfg.BoxRunURL, "/"),
		b.boxID,
	)
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, startURL, bytes.NewReader(body),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	b.setAuth(req)
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf(
			"boxrun exec start status=%d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(raw)),
		)
	}
	var started struct {
		ExecutionID string `json:"execution_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&started); err != nil {
		return "", err
	}
	if started.ExecutionID == "" {
		return "", fmt.Errorf("boxrun exec: missing execution_id")
	}
	outURL := fmt.Sprintf(
		"%s/boxes/%s/executions/%s/output",
		strings.TrimRight(b.cfg.BoxRunURL, "/"),
		b.boxID,
		started.ExecutionID,
	)
	outReq, err := http.NewRequestWithContext(ctx, http.MethodGet, outURL, nil)
	if err != nil {
		return "", err
	}
	outReq.Header.Set("Accept", "text/event-stream")
	b.setAuth(outReq)
	outResp, err := b.httpClient.Do(outReq)
	if err != nil {
		return "", err
	}
	defer outResp.Body.Close()
	if outResp.StatusCode >= 300 {
		return "", fmt.Errorf("boxrun exec stream status=%d", outResp.StatusCode)
	}
	stdout, stderr, exitCode, err := parseBoxRunSSE(outResp.Body)
	if err != nil {
		return "", err
	}
	combined := strings.TrimRight(stdout+stderr, "\n")
	if exitCode != 0 {
		return combined + fmt.Sprintf("\n[exit %d]", exitCode), nil
	}
	return combined, nil
}

// ReadFile implements Executor via tar download.
func (b *BoxRunExecutor) ReadFile(ctx context.Context, path string) ([]byte, error) {
	if err := b.waitBox(ctx); err != nil {
		return nil, err
	}
	normalized := normalizeVMPath(path)
	fileURL := fmt.Sprintf(
		"%s/boxes/%s/files?path=%s",
		strings.TrimRight(b.cfg.BoxRunURL, "/"),
		b.boxID,
		url.QueryEscape(normalized),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/x-tar")
	b.setAuth(req)
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf(
			"boxrun read status=%d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(raw)),
		)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return extractFirstTarFile(data)
}

// Destroy deletes the remote box.
func (b *BoxRunExecutor) Destroy(ctx context.Context) error {
	if b.boxID == "" {
		select {
		case <-b.boxReady:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if b.boxID == "" {
		return nil
	}
	delURL := fmt.Sprintf(
		"%s/boxes/%s",
		strings.TrimRight(b.cfg.BoxRunURL, "/"),
		b.boxID,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, delURL, nil)
	if err != nil {
		return err
	}
	b.setAuth(req)
	resp, err := b.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 && resp.StatusCode != http.StatusNotFound {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf(
			"boxrun delete status=%d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(raw)),
		)
	}
	b.boxID = ""
	return nil
}

func (b *BoxRunExecutor) waitBox(ctx context.Context) error {
	select {
	case <-b.boxReady:
		return b.boxErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (b *BoxRunExecutor) createBox(ctx context.Context) {
	defer close(b.boxReady)
	body := map[string]any{
		"image": b.cfg.SandboxImage,
		"name":  fmt.Sprintf("oma-%s", truncateID(b.sessionID, 30)),
	}
	if b.cfg.BoxRunCPUs > 0 {
		body["cpus"] = b.cfg.BoxRunCPUs
	}
	if b.cfg.BoxRunMemoryMib > 0 {
		body["memory_mib"] = b.cfg.BoxRunMemoryMib
	}
	payload, _ := json.Marshal(body)
	createURL := strings.TrimRight(b.cfg.BoxRunURL, "/") + "/boxes"
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, createURL, bytes.NewReader(payload),
	)
	if err != nil {
		b.boxErr = err
		return
	}
	req.Header.Set("Content-Type", "application/json")
	b.setAuth(req)
	resp, err := b.httpClient.Do(req)
	if err != nil {
		b.boxErr = err
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		b.boxErr = fmt.Errorf(
			"boxrun create status=%d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(raw)),
		)
		return
	}
	var created struct {
		BoxID string `json:"box_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		b.boxErr = err
		return
	}
	if created.BoxID == "" {
		b.boxErr = fmt.Errorf("boxrun create: missing box_id")
		return
	}
	b.boxID = created.BoxID
}

func (b *BoxRunExecutor) setAuth(req *http.Request) {
	if b.cfg.BoxRunToken != "" {
		req.Header.Set("Authorization", "Bearer "+b.cfg.BoxRunToken)
	}
}

func normalizeVMPath(path string) string {
	if strings.HasPrefix(path, "/") {
		return path
	}
	return "/workspace/" + strings.TrimPrefix(path, "/")
}

func truncateID(id string, max int) string {
	if len(id) <= max {
		return id
	}
	return id[:max]
}

func parseBoxRunSSE(r io.Reader) (stdout, stderr string, exitCode int, err error) {
	raw, readErr := io.ReadAll(r)
	if readErr != nil {
		return "", "", 0, readErr
	}
	blocks := strings.Split(string(raw), "\n\n")
	for _, block := range blocks {
		evType, payload := parseSSEBlock(block)
		if payload == nil {
			continue
		}
		switch evType {
		case "stdout", "stderr":
			if data, ok := payload["data"].(string); ok {
				decoded, decErr := decodeBase64(data)
				if decErr != nil {
					continue
				}
				if evType == "stdout" {
					stdout += decoded
				} else {
					stderr += decoded
				}
			}
		case "exit":
			if code, ok := payload["exit_code"].(float64); ok {
				exitCode = int(code)
			}
		}
	}
	return stdout, stderr, exitCode, nil
}

func parseSSEBlock(block string) (string, map[string]any) {
	block = strings.TrimSpace(block)
	if block == "" {
		return "", nil
	}
	evType := "message"
	var dataStr string
	for _, line := range strings.Split(block, "\n") {
		if strings.HasPrefix(line, "event:") {
			evType = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		} else if strings.HasPrefix(line, "data:") {
			dataStr += strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		}
	}
	if dataStr == "" {
		return "", nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(dataStr), &payload); err != nil {
		return "", nil
	}
	return evType, payload
}

func extractFirstTarFile(tarBytes []byte) ([]byte, error) {
	tr := tar.NewReader(bytes.NewReader(tarBytes))
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeReg || hdr.Typeflag == 0 {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("tar archive contained no regular file")
}
