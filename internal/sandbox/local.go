package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// LocalExecutor runs commands in a host workdir (LocalSubprocess parity).
type LocalExecutor struct {
	workdir string
}

// NewLocalExecutor returns a host workdir executor.
func NewLocalExecutor(workdirPath string) *LocalExecutor {
	return &LocalExecutor{workdir: workdirPath}
}

// Provider implements Executor.
func (*LocalExecutor) Provider() string {
	return ProviderLocal
}

// Exec implements Executor.
func (l *LocalExecutor) Exec(
	ctx context.Context,
	command string,
	timeout time.Duration,
) (string, error) {
	if l.workdir == "" {
		return "", fmt.Errorf("workdir not configured")
	}
	if err := os.MkdirAll(l.workdir, 0o755); err != nil {
		return "", err
	}
	if timeout <= 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	cmd.Dir = l.workdir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	combined := strings.TrimRight(
		stdout.String()+stderr.String(),
		"\n",
	)
	exitCode := 0
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			exitCode = ee.ExitCode()
		} else if ctx.Err() != nil {
			return combined + "\n[exit 124]", nil
		} else {
			return "", err
		}
	}
	if exitCode != 0 {
		return combined + fmt.Sprintf("\n[exit %d]", exitCode), nil
	}
	return combined, nil
}

// ReadFile implements Executor.
func (l *LocalExecutor) ReadFile(_ context.Context, sandboxPath string) ([]byte, error) {
	abs, err := resolveLocalSandboxPath(l.workdir, sandboxPath)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(abs)
}

func resolveLocalSandboxPath(workdirPath, sandboxPath string) (string, error) {
	if strings.Contains(sandboxPath, "..") {
		return "", fmt.Errorf("invalid sandbox path")
	}
	rel := sandboxPath
	if strings.HasPrefix(rel, "/workspace/") {
		rel = rel[len("/workspace/"):]
	} else if rel == "/workspace" {
		rel = ""
	} else if strings.HasPrefix(rel, "/") {
		rel = rel[1:]
	}
	candidate := filepath.Join(workdirPath, filepath.FromSlash(rel))
	absWorkdir, err := filepath.Abs(workdirPath)
	if err != nil {
		return "", err
	}
	absCandidate, err := filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	if absCandidate != absWorkdir &&
		!strings.HasPrefix(absCandidate, absWorkdir+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid sandbox path")
	}
	return absCandidate, nil
}

// Destroy is a no-op for local workdirs (platform removes the directory).
func (*LocalExecutor) Destroy(context.Context) error {
	return nil
}
