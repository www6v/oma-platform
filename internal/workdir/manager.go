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
	base string
}

// NewManager returns a workdir manager rooted at base.
func NewManager(base string) *Manager {
	return &Manager{base: base}
}

// Ensure creates the session directory if needed.
func (m *Manager) Ensure(_ context.Context, sessionID string) (string, error) {
	if err := validateSessionID(sessionID); err != nil {
		return "", err
	}
	path := filepath.Join(m.base, sessionID)
	if err := os.MkdirAll(path, 0o755); err != nil {
		return "", fmt.Errorf("mkdir workdir: %w", err)
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

func validateSessionID(sessionID string) error {
	if sessionID == "" {
		return fmt.Errorf("session id required")
	}
	if strings.Contains(sessionID, "..") || strings.ContainsAny(sessionID, `/\`) {
		return fmt.Errorf("invalid session id")
	}
	return nil
}
