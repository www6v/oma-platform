package workdir

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Manager provisions per-session working directories.
type Manager struct {
	base        string
	outputsRoot string
}

// NewManager returns a workdir manager rooted at base. When outputsRoot is
// non-empty, Ensure also mounts session outputs at .mnt/session/outputs.
func NewManager(base, outputsRoot string) *Manager {
	return &Manager{base: base, outputsRoot: outputsRoot}
}

// Ensure creates the session directory and mounts session outputs when
// outputsRoot is configured.
func (m *Manager) Ensure(
	_ context.Context,
	tenantID, sessionID string,
) (string, error) {
	if err := validateSessionID(sessionID); err != nil {
		return "", err
	}
	path := filepath.Join(m.base, sessionID)
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", fmt.Errorf("mkdir workdir: %w", err)
	}
	if m.outputsRoot != "" {
		if err := mountSessionOutputs(path, m.outputsRoot, tenantID, sessionID); err != nil {
			return "", err
		}
	}
	return path, nil
}

// Remove deletes the session workdir.
func (m *Manager) Remove(_ context.Context, sessionID string) error {
	if err := validateSessionID(sessionID); err != nil {
		return err
	}
	path := filepath.Join(m.base, sessionID)
	if err := os.RemoveAll(path); err != nil {
		return fmt.Errorf("remove workdir: %w", err)
	}
	return nil
}

func mountSessionOutputs(
	workdir, outputsRoot, tenantID, sessionID string,
) error {
	targetDir := filepath.Join(outputsRoot, normalizeTenant(tenantID), sessionID)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("mkdir session outputs: %w", err)
	}
	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return fmt.Errorf("abs session outputs: %w", err)
	}

	mountParent := filepath.Join(workdir, ".mnt", "session")
	if err := os.MkdirAll(mountParent, 0o755); err != nil {
		return fmt.Errorf("mkdir outputs mount parent: %w", err)
	}
	workdirLink := filepath.Join(mountParent, "outputs")
	if err := replaceSymlink(workdirLink, absTarget); err != nil {
		return fmt.Errorf("symlink %s: %w", workdirLink, err)
	}

	// Short alias so agents can write outputs/report.md (AMA local-subprocess
	// also exposes OMA_OUTPUTS_DIR at the workdir-relative mount).
	rootAlias := filepath.Join(workdir, "outputs")
	if err := replaceSymlink(rootAlias, absTarget); err != nil {
		return fmt.Errorf("symlink %s: %w", rootAlias, err)
	}

	tryRootSessionOutputsMount(absTarget)
	return nil
}

func tryRootSessionOutputsMount(targetDir string) {
	sessionDir := "/mnt/session"
	outputsLink := "/mnt/session/outputs"
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		return
	}
	_ = replaceSymlink(outputsLink, targetDir)
}

func replaceSymlink(link, target string) error {
	_ = os.Remove(link)
	return os.Symlink(target, link)
}

func normalizeTenant(tenantID string) string {
	if tenantID == "" {
		return "default"
	}
	return tenantID
}

func validateSessionID(sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("session id required")
	}
	if strings.Contains(sessionID, "..") ||
		strings.ContainsAny(sessionID, `/\`) {
		return fmt.Errorf("invalid session id")
	}
	return nil
}
