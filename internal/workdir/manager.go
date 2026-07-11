package workdir

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/open-ma/oma-building/internal/sandbox"
)

// Manager provisions per-session working directories.
type Manager struct {
	base        string
	outputsRoot string
	memoryRoot  string
	Backup      *BackupService
	Sandbox     *sandbox.Registry
}

// NewManager returns a workdir manager rooted at base. When outputsRoot is
// non-empty, Ensure also mounts session outputs at .mnt/session/outputs.
// When memoryRoot is non-empty, Ensure mounts memory stores at .mnt/memory.
func NewManager(base, outputsRoot, memoryRoot string) *Manager {
	return &Manager{
		base:        base,
		outputsRoot: outputsRoot,
		memoryRoot:  memoryRoot,
	}
}

// Ensure creates the session directory and mounts session outputs when
// outputsRoot is configured. Memory mounts symlink into memoryRoot/storeId.
func (m *Manager) Ensure(
	_ context.Context,
	tenantID, sessionID string,
	memoryMounts []MemoryMount,
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
	if err := mountMemoryStores(path, m.memoryRoot, memoryMounts); err != nil {
		return "", err
	}
	return path, nil
}

// BaseDir returns the sandbox workdir root.
func (m *Manager) BaseDir() string {
	return m.base
}

// MemoryRoot returns the host memory blob root.
func (m *Manager) MemoryRoot() string {
	return m.memoryRoot
}

// OutputsRoot returns the session outputs root.
func (m *Manager) OutputsRoot() string {
	return m.outputsRoot
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

	// Bash and some agents write to mnt/session/outputs (no leading dot)
	// when the host-level /mnt/session mount is absent.
	mntSessionDir := filepath.Join(workdir, "mnt", "session")
	if err := os.MkdirAll(mntSessionDir, 0o755); err != nil {
		return fmt.Errorf("mkdir mnt/session: %w", err)
	}
	mntOutputsLink := filepath.Join(mntSessionDir, "outputs")
	if err := replaceSymlink(mntOutputsLink, absTarget); err != nil {
		return fmt.Errorf("symlink %s: %w", mntOutputsLink, err)
	}

	tryRootSessionOutputsMount(absTarget)
	return nil
}

func tryRootSessionOutputsMount(targetDir string) {
	sessionDir := "/mnt/session"
	outputsLink := filepath.Join(sessionDir, "outputs")
	info, err := os.Lstat(sessionDir)
	if err != nil || !info.IsDir() {
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
