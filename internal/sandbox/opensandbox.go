package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// OpenSandboxExecutor runs commands in an OpenSandbox container.
//
// OpenSandbox exposes two HTTP surfaces:
//  1. A Lifecycle Server (default port 18090) for create/delete of sandboxes.
//  2. An execd daemon (default port 44772) inside each sandbox for command /
//     file operations. The Lifecycle Server proxies execd through
//     `GET /v1/sandboxes/{id}/endpoints/{port}?use_server_proxy=true`, which
//     returns an endpoint URL + headers that this executor calls directly.
type OpenSandboxExecutor struct {
	cfg          Config
	httpClient   *http.Client
	sandboxID    string
	execdURL     string // e.g. "http://host:18090/sandboxes/<id>/proxy/44772"
	execdHeaders http.Header
}

// --- wire types -------------------------------------------------------------

type osCreateRequest struct {
	Image          osImageSpec        `json:"image"`
	Entrypoint     []string           `json:"entrypoint,omitempty"`
	Timeout        int                `json:"timeout,omitempty"`
	ResourceLimits map[string]string  `json:"resourceLimits,omitempty"`
	Env            map[string]string  `json:"env,omitempty"`
	Metadata       map[string]string  `json:"metadata,omitempty"`
}

type osImageSpec struct {
	URI string `json:"uri"`
}

type osCreateResponse struct {
	ID        string         `json:"id"`
	Status    *osStatus      `json:"status"`
	CreatedAt string         `json:"createdAt"`
	Metadata  map[string]any `json:"metadata"`
}

type osStatus struct {
	State string `json:"state"`
}

type osEndpointResponse struct {
	Endpoint string            `json:"endpoint"`
	Headers  map[string]string `json:"headers"`
}

type osCommandRequest struct {
	Command    string            `json:"command"`
	Cwd        string            `json:"cwd,omitempty"`
	Background bool              `json:"background"`
	TimeoutMs  int64             `json:"timeout,omitempty"`
	Envs       map[string]string `json:"envs,omitempty"`
}

type osExecEvent struct {
	Type          string `json:"type"`
	Text          string `json:"text"`
	ExitCode      *int   `json:"exit_code,omitempty"`
	ExecutionTime *int64 `json:"execution_time,omitempty"`
	Error         *struct {
		EName  string `json:"ename"`
		EValue string `json:"evalue"`
	} `json:"error,omitempty"`
}

// --- constructor ------------------------------------------------------------

// NewOpenSandboxExecutor creates a remote OpenSandbox sandbox and resolves
// the execd endpoint for subsequent command execution.
func NewOpenSandboxExecutor(
	ctx context.Context,
	cfg Config,
	opts AcquireOpts,
	httpClient *http.Client,
) (*OpenSandboxExecutor, error) {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	lifecycle := openSandboxLifecycleBase(cfg)
	apiKey := cfg.OpenSandboxAPIKey

	image := cfg.OpenSandboxImage
	if image == "" {
		image = "python:3.12"
	}
	timeout := cfg.OpenSandboxTimeoutSec
	if timeout <= 0 {
		timeout = 3600
	}
	cpu := cfg.OpenSandboxCPU
	if cpu == "" {
		cpu = "500m"
	}
	mem := cfg.OpenSandboxMemory
	if mem == "" {
		mem = "512Mi"
	}

	metadata := map[string]string{
		"oma_session_id": opts.SessionID,
	}
	if opts.TenantID != "" {
		metadata["oma_tenant_id"] = opts.TenantID
	}

	env := map[string]string{}
	if opts.SessionID != "" {
		env["OMA_SESSION_ID"] = opts.SessionID
	}

	body := osCreateRequest{
		Image:          osImageSpec{URI: image},
		Timeout:        timeout,
		ResourceLimits: map[string]string{"cpu": cpu, "memory": mem},
		Env:            env,
		Metadata:       metadata,
	}
	if cfg.OpenSandboxEntrypoint != "" {
		body.Entrypoint = []string{cfg.OpenSandboxEntrypoint}
	} else {
		// The Lifecycle Server rejects creates without an entrypoint.
		// Match the Python SDK default: a keep-alive that parks the
		// container until we exec into it.
		body.Entrypoint = []string{"tail", "-f", "/dev/null"}
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, lifecycle+"/v1/sandboxes",
		bytes.NewReader(payload),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("OPEN-SANDBOX-API-KEY", apiKey)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("opensandbox create: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf(
			"opensandbox create status=%d: %s",
			resp.StatusCode, strings.TrimSpace(string(raw)),
		)
	}
	var created osCreateResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		return nil, fmt.Errorf("opensandbox create decode: %w", err)
	}
	if created.ID == "" {
		return nil, fmt.Errorf("opensandbox create: missing sandbox id")
	}
	sandboxID := created.ID

	// Cleanup on any failure past this point — we've already paid for a
	// container and don't want to leak it.
	success := false
	defer func() {
		if !success {
			cleanupCtx, cancel := context.WithTimeout(
				context.Background(), 15*time.Second,
			)
			defer cancel()
			_ = openSandboxDelete(cleanupCtx, httpClient, lifecycle, apiKey, sandboxID)
		}
	}()

	// Resolve execd endpoint.
	execdURL, execdHeaders, err := openSandboxResolveExecd(
		ctx, httpClient, lifecycle, apiKey, sandboxID, cfg,
	)
	if err != nil {
		return nil, err
	}

	// Poll execd /ping until ready.
	if err := openSandboxWaitForExecd(
		ctx, httpClient, execdURL, execdHeaders,
	); err != nil {
		return nil, err
	}

	// The default image (python:3.12) has no /workspace, but agents
	// assume it exists (other providers create it server-side). Make it
	// so ReadFile/Write-file paths work consistently across providers.
	initExec := openSandboxExecOnce(
		ctx, httpClient, execdURL, execdHeaders,
		"mkdir -p /workspace", 10*time.Second,
	)
	if initExec != "" {
		// Non-fatal — log-style ignore. Some images already have it.
		_ = initExec
	}

	success = true
	return &OpenSandboxExecutor{
		cfg:          cfg,
		httpClient:   httpClient,
		sandboxID:    sandboxID,
		execdURL:     execdURL,
		execdHeaders: execdHeaders,
	}, nil
}

// Provider implements Executor.
func (*OpenSandboxExecutor) Provider() string {
	return ProviderOpenSandbox
}

// Exec implements Executor via execd POST /command + SSE stream.
func (o *OpenSandboxExecutor) Exec(
	ctx context.Context,
	command string,
	timeout time.Duration,
) (string, error) {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	body := osCommandRequest{
		Command:   command,
		// OpenSandbox's execd validates cwd strictly and the default
		// image (python:3.12) has no /workspace. Leave Cwd empty so the
		// server falls back to the container's own workdir; callers who
		// need a specific directory should `cd` in the command.
		TimeoutMs: timeout.Milliseconds(),
	}
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, o.execdURL+"/command",
		bytes.NewReader(payload),
	)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	o.applyExecdHeaders(req)

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("opensandbox exec: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", fmt.Errorf(
			"opensandbox exec status=%d: %s",
			resp.StatusCode, strings.TrimSpace(string(raw)),
		)
	}
	return parseOpenSandboxSSE(resp.Body)
}

// ReadFile implements Executor via execd GET /files/download with a base64
// shell fallback for older execd builds that don't expose the endpoint.
func (o *OpenSandboxExecutor) ReadFile(
	ctx context.Context,
	path string,
) ([]byte, error) {
	normalized := path
	if !strings.HasPrefix(normalized, "/") {
		normalized = "/workspace/" + strings.TrimPrefix(normalized, "/")
	}
	reqURL := o.execdURL + "/files/download?path=" +
		url.QueryEscape(normalized)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	o.applyExecdHeaders(req)
	resp, err := o.httpClient.Do(req)
	if err != nil {
		// Network error — fall back to base64 shell.
		return o.readFileFallback(ctx, normalized)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent {
		return io.ReadAll(resp.Body)
	}
	// 4xx/5xx — fall back.
	_, _ = io.Copy(io.Discard, resp.Body)
	return o.readFileFallback(ctx, normalized)
}

// Destroy deletes the remote sandbox. Idempotent.
func (o *OpenSandboxExecutor) Destroy(ctx context.Context) error {
	if o.sandboxID == "" {
		return nil
	}
	err := openSandboxDelete(
		ctx, o.httpClient,
		openSandboxLifecycleBase(o.cfg),
		o.cfg.OpenSandboxAPIKey,
		o.sandboxID,
	)
	if err == nil {
		o.sandboxID = ""
	}
	return err
}

// --- internals --------------------------------------------------------------

func (o *OpenSandboxExecutor) applyExecdHeaders(req *http.Request) {
	if o.execdHeaders == nil {
		return
	}
	for k, vs := range o.execdHeaders {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
}

func (o *OpenSandboxExecutor) readFileFallback(
	ctx context.Context,
	path string,
) ([]byte, error) {
	out, err := o.Exec(
		ctx, fmt.Sprintf("base64 -w0 %q", path), 30*time.Second,
	)
	if err != nil {
		return nil, err
	}
	if strings.Contains(out, "[exit ") {
		return nil, fmt.Errorf("opensandbox read fallback: %s", out)
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return []byte{}, nil
	}
	return decodeBase64Loose(trimmed)
}

func openSandboxLifecycleBase(cfg Config) string {
	proto := cfg.OpenSandboxProtocol
	if proto == "" {
		proto = "http"
	}
	return proto + "://" + strings.TrimRight(cfg.OpenSandboxDomain, "/")
}

func openSandboxDelete(
	ctx context.Context,
	httpClient *http.Client,
	lifecycle, apiKey, sandboxID string,
) error {
	if sandboxID == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodDelete,
		lifecycle+"/v1/sandboxes/"+url.PathEscape(sandboxID),
		nil,
	)
	if err != nil {
		return err
	}
	if apiKey != "" {
		req.Header.Set("OPEN-SANDBOX-API-KEY", apiKey)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return nil
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf(
			"opensandbox delete status=%d", resp.StatusCode,
		)
	}
	return nil
}

func openSandboxResolveExecd(
	ctx context.Context,
	httpClient *http.Client,
	lifecycle, apiKey, sandboxID string,
	cfg Config,
) (string, http.Header, error) {
	port := cfg.OpenSandboxExecdPort
	if port <= 0 {
		port = 44772
	}
	q := url.Values{}
	if cfg.OpenSandboxUseServerProxy {
		q.Set("use_server_proxy", "true")
	}
	reqURL := fmt.Sprintf(
		"%s/v1/sandboxes/%s/endpoints/%d?%s",
		lifecycle,
		url.PathEscape(sandboxID),
		port,
		q.Encode(),
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return "", nil, err
	}
	if apiKey != "" {
		req.Header.Set("OPEN-SANDBOX-API-KEY", apiKey)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("opensandbox endpoint resolve: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return "", nil, fmt.Errorf(
			"opensandbox endpoint resolve status=%d: %s",
			resp.StatusCode, strings.TrimSpace(string(raw)),
		)
	}
	var ep osEndpointResponse
	if err := json.NewDecoder(resp.Body).Decode(&ep); err != nil {
		return "", nil, fmt.Errorf("opensandbox endpoint decode: %w", err)
	}
	if ep.Endpoint == "" {
		return "", nil, fmt.Errorf("opensandbox endpoint: empty endpoint URL")
	}
	// The Lifecycle Server sometimes returns the endpoint without a scheme
	// (e.g. "124.221.28.203:18090/sandboxes/.../proxy/44772"). Inherit the
	// lifecycle scheme so http.NewRequest can parse it.
	endpoint := strings.TrimRight(ep.Endpoint, "/")
	if !strings.Contains(endpoint, "://") {
		proto := cfg.OpenSandboxProtocol
		if proto == "" {
			proto = "http"
		}
		endpoint = proto + "://" + endpoint
	}
	hdr := http.Header{}
	for k, v := range ep.Headers {
		hdr.Set(k, v)
	}
	return endpoint, hdr, nil
}

func openSandboxWaitForExecd(
	ctx context.Context,
	httpClient *http.Client,
	execdURL string,
	execdHeaders http.Header,
) error {
	deadline := time.Now().Add(15 * time.Second)
	backoff := 200 * time.Millisecond
	for {
		req, err := http.NewRequestWithContext(
			ctx, http.MethodGet, execdURL+"/ping", nil,
		)
		if err != nil {
			return err
		}
		for k, vs := range execdHeaders {
			for _, v := range vs {
				req.Header.Set(k, v)
			}
		}
		resp, err := httpClient.Do(req)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf(
				"opensandbox execd not ready after 15s (last err=%v)", err,
			)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		if backoff < 2*time.Second {
			backoff *= 2
		}
	}
}

// openSandboxExecOnce is a small helper used during sandbox init to run a
// single setup command (e.g. mkdir -p /workspace) via execd. It returns the
// combined output on any failure; empty string on success (exit 0).
func openSandboxExecOnce(
	ctx context.Context,
	httpClient *http.Client,
	execdURL string,
	execdHeaders http.Header,
	command string,
	timeout time.Duration,
) string {
	body := osCommandRequest{
		Command:   command,
		TimeoutMs: timeout.Milliseconds(),
	}
	payload, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, execdURL+"/command",
		bytes.NewReader(payload),
	)
	if err != nil {
		return err.Error()
	}
	req.Header.Set("Content-Type", "application/json")
	for k, vs := range execdHeaders {
		for _, v := range vs {
			req.Header.Set(k, v)
		}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err.Error()
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return strings.TrimSpace(string(raw))
	}
	out, _ := parseOpenSandboxSSE(resp.Body)
	if strings.Contains(out, "[exit ") {
		return out
	}
	return ""
}

// parseOpenSandboxSSE consumes the execd /command SSE stream. Events are
// `data: {...}` lines; we care about stdout/stderr/execution_complete/error.
func parseOpenSandboxSSE(r io.Reader) (string, error) {
	var stdout, stderr strings.Builder
	exitCode := 0
	scanner := bufio.NewScanner(r)
	// SSE lines can exceed the default 64KB buffer when commands produce
	// very long output; raise to 1MB.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		if line[0] == ':' {
			continue // SSE comment / keepalive
		}
		var raw []byte
		const prefix = "data: "
		if bytes.HasPrefix(line, []byte(prefix)) {
			raw = line[len(prefix):]
		} else if line[0] == '{' {
			// The live execd emits bare JSON objects separated by
			// blank lines, with no `data:` prefix.
			raw = line
		} else {
			continue
		}
		var ev osExecEvent
		if err := json.Unmarshal(raw, &ev); err != nil {
			continue
		}
	switch ev.Type {
		case "stdout":
			stdout.WriteString(ev.Text)
			if len(ev.Text) > 0 && ev.Text[len(ev.Text)-1] != '\n' {
				stdout.WriteString("\n")
			}
		case "stderr":
			stderr.WriteString(ev.Text)
			if len(ev.Text) > 0 && ev.Text[len(ev.Text)-1] != '\n' {
				stderr.WriteString("\n")
			}
		case "error":
			if ev.Error != nil {
				// execd reports non-zero shell exits as a
				// CommandExecError with the code in evalue, then
				// closes the stream (no execution_complete event).
				if ev.Error.EName == "CommandExecError" {
					if code, err := strconv.Atoi(ev.Error.EValue); err == nil {
						exitCode = code
					}
				} else {
					stderr.WriteString(ev.Error.EName + ": " + ev.Error.EValue + "\n")
				}
			}
		case "execution_complete":
			if ev.ExitCode != nil {
				exitCode = *ev.ExitCode
			}
			// Foreground commands: the server may keep the HTTP
			// connection alive after execution_complete, so stop
			// here rather than blocking on scanner.Scan() forever.
			return combineOpenSandboxOutput(stdout.String(), stderr.String(), exitCode), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("opensandbox sse read: %w", err)
	}
	// Stream ended before execution_complete — best-effort output.
	return combineOpenSandboxOutput(stdout.String(), stderr.String(), exitCode), nil
}

func combineOpenSandboxOutput(stdout, stderr string, exitCode int) string {
	combined := strings.TrimRight(stdout+stderr, "\n")
	if exitCode != 0 {
		return combined + fmt.Sprintf("\n[exit %d]", exitCode)
	}
	return combined
}

func decodeBase64Loose(s string) ([]byte, error) {
	// Some shells append a trailing newline; trim before decoding.
	return base64.StdEncoding.DecodeString(strings.TrimSpace(s))
}
