package sandbox

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// LiteBoxExecutor runs commands in a local BoxLite micro-VM via Node bridge.
type LiteBoxExecutor struct {
	cfg        Config
	sessionID  string
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	scanner    *bufio.Scanner
	mu         sync.Mutex
	nextID     atomic.Int64
	initialized bool
}

// VolumeMount binds a host directory into the LiteBox guest.
type VolumeMount struct {
	HostPath  string
	GuestPath string
	ReadOnly  bool
}

// AcquireOpts carries per-session sandbox provisioning context.
type AcquireOpts struct {
	SessionID    string
	WorkdirPath  string
	TenantID     string
	MemoryRoot   string
	OutputsRoot  string
	MemoryMounts []MemoryMount
}

// MemoryMount describes a memory store bind for remote sandboxes.
type MemoryMount struct {
	StoreName string
	StoreID   string
	ReadOnly  bool
}

// NewLiteBoxExecutor starts the Node bridge process.
func NewLiteBoxExecutor(
	ctx context.Context,
	cfg Config,
	opts AcquireOpts,
) (*LiteBoxExecutor, error) {
	bridgePath, err := resolveLiteboxBridgePath()
	if err != nil {
		return nil, err
	}
	nodeBin := envOrDefault("NODE_BIN", "node")
	cmd := exec.CommandContext(ctx, nodeBin, bridgePath)
	cmd.Env = os.Environ()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("litebox bridge start: %w", err)
	}
	ex := &LiteBoxExecutor{
		cfg:       cfg,
		sessionID: opts.SessionID,
		cmd:       cmd,
		stdin:     stdin,
		scanner:   bufio.NewScanner(stdout),
	}
	volumes := buildLiteboxVolumes(cfg, opts)
	initBody := map[string]any{
		"op":    "init",
		"image": cfg.SandboxImage,
		"name":  fmt.Sprintf("oma-%s", opts.SessionID),
		"volumes": volumes,
	}
	if cfg.LiteBoxMemoryMib > 0 {
		initBody["memoryMib"] = cfg.LiteBoxMemoryMib
	}
	if cfg.LiteBoxCPUs > 0 {
		initBody["cpus"] = cfg.LiteBoxCPUs
	}
	if _, err := ex.call(ctx, initBody); err != nil {
		_ = ex.Destroy(ctx)
		return nil, err
	}
	ex.initialized = true
	return ex, nil
}

// Provider implements Executor.
func (*LiteBoxExecutor) Provider() string {
	return ProviderLiteBox
}

// Exec implements Executor.
func (l *LiteBoxExecutor) Exec(
	ctx context.Context,
	command string,
	timeout time.Duration,
) (string, error) {
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	resp, err := l.call(ctx, map[string]any{
		"op":        "exec",
		"command":   command,
		"timeoutMs": int(timeout.Milliseconds()),
	})
	if err != nil {
		return "", err
	}
	out, _ := resp["output"].(string)
	return out, nil
}

// ReadFile implements Executor via copyOut bridge.
func (l *LiteBoxExecutor) ReadFile(ctx context.Context, path string) ([]byte, error) {
	resp, err := l.call(ctx, map[string]any{
		"op":   "readFile",
		"path": path,
	})
	if err != nil {
		return nil, err
	}
	enc, _ := resp["data"].(string)
	if enc == "" {
		return nil, fmt.Errorf("litebox readFile: empty data")
	}
	return base64.StdEncoding.DecodeString(enc)
}

// Destroy stops the micro-VM and bridge process.
func (l *LiteBoxExecutor) Destroy(ctx context.Context) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.stdin != nil && l.initialized {
		_, _ = l.callLocked(ctx, map[string]any{"op": "destroy"})
	}
	if l.stdin != nil {
		_ = l.stdin.Close()
		l.stdin = nil
	}
	if l.cmd != nil && l.cmd.Process != nil {
		_ = l.cmd.Process.Kill()
		_, _ = l.cmd.Process.Wait()
	}
	return nil
}

func (l *LiteBoxExecutor) call(
	ctx context.Context,
	body map[string]any,
) (map[string]any, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.callLocked(ctx, body)
}

func (l *LiteBoxExecutor) callLocked(
	ctx context.Context,
	body map[string]any,
) (map[string]any, error) {
	if l.stdin == nil {
		return nil, fmt.Errorf("litebox bridge not running")
	}
	id := l.nextID.Add(1)
	body["id"] = id
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() {
		_, werr := fmt.Fprintf(l.stdin, "%s\n", payload)
		done <- werr
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-done:
		if err != nil {
			return nil, err
		}
	}
	for l.scanner.Scan() {
		var resp map[string]any
		if err := json.Unmarshal(l.scanner.Bytes(), &resp); err != nil {
			continue
		}
		respID, _ := resp["id"].(float64)
		if int64(respID) != id {
			continue
		}
		if ok, _ := resp["ok"].(bool); !ok {
			msg, _ := resp["error"].(string)
			return nil, fmt.Errorf("litebox bridge: %s", msg)
		}
		return resp, nil
	}
	if err := l.scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("litebox bridge: no response")
}

func buildLiteboxVolumes(cfg Config, opts AcquireOpts) []map[string]any {
	var volumes []map[string]any
	memoryRoot := opts.MemoryRoot
	if memoryRoot == "" {
		memoryRoot = os.Getenv("MEMORY_DATA_DIR")
	}
	for _, mount := range opts.MemoryMounts {
		if memoryRoot == "" {
			continue
		}
		hostPath := filepath.Join(memoryRoot, mount.StoreID)
		_ = os.MkdirAll(hostPath, 0o755)
		volumes = append(volumes, map[string]any{
			"hostPath":  hostPath,
			"guestPath": fmt.Sprintf("/mnt/memory/%s", mount.StoreName),
			"readOnly":  mount.ReadOnly,
		})
	}
	outputsRoot := opts.OutputsRoot
	if outputsRoot == "" {
		outputsRoot = os.Getenv("SESSION_OUTPUTS_DIR")
	}
	if outputsRoot != "" && opts.TenantID != "" && opts.SessionID != "" {
		hostPath := filepath.Join(outputsRoot, opts.TenantID, opts.SessionID)
		_ = os.MkdirAll(hostPath, 0o755)
		volumes = append(volumes, map[string]any{
			"hostPath":  hostPath,
			"guestPath": "/mnt/session/outputs",
			"readOnly":  false,
		})
	}
	return volumes
}

func resolveLiteboxBridgePath() (string, error) {
	if custom := os.Getenv("LITEBOX_BRIDGE_PATH"); custom != "" {
		if _, err := os.Stat(custom); err == nil {
			return custom, nil
		}
	}
	candidates := []string{
		"scripts/sandbox/litebox-bridge.mjs",
	}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(
			candidates,
			filepath.Join(wd, "scripts/sandbox/litebox-bridge.mjs"),
		)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(
			candidates,
			filepath.Join(filepath.Dir(exe), "scripts/sandbox/litebox-bridge.mjs"),
		)
	}
	for _, c := range candidates {
		abs, err := filepath.Abs(c)
		if err != nil {
			continue
		}
		if _, err := os.Stat(abs); err == nil {
			return abs, nil
		}
	}
	return "", fmt.Errorf(
		"litebox bridge not found; run npm install in scripts/sandbox " +
			"or set LITEBOX_BRIDGE_PATH",
	)
}

func normalizeProviderName(provider string) string {
	p := strings.ToLower(strings.TrimSpace(provider))
	switch p {
	case "boxlite", "litebox":
		return ProviderLiteBox
	case "subprocess":
		return ProviderLocal
	default:
		return p
	}
}
