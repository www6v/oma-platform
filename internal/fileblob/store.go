package fileblob

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Store persists uploaded file bytes on local disk.
type Store struct {
	root string
}

// NewStore returns a filesystem-backed file blob store.
func NewStore(root string) *Store {
	return &Store{root: root}
}

// Key returns the AMA-compatible blob key: t/{tenant}/files/{fileID}.
func Key(tenantID, fileID string) string {
	tenant := tenantID
	if tenant == "" {
		tenant = "default"
	}
	return filepath.Join("t", tenant, "files", fileID)
}

// Write stores bytes and returns the blob key.
func (s *Store) Write(tenantID, fileID string, data []byte) (string, error) {
	key := Key(tenantID, fileID)
	path, err := s.resolve(key)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return key, nil
}

// Read loads blob bytes by key.
func (s *Store) Read(key string) ([]byte, error) {
	path, err := s.resolve(key)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

// Delete removes a blob by key. Missing files are ignored.
func (s *Store) Delete(key string) error {
	path, err := s.resolve(key)
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (s *Store) resolve(key string) (string, error) {
	if key == "" || strings.Contains(key, "..") {
		return "", fmt.Errorf("invalid blob key")
	}
	full := filepath.Join(s.root, filepath.FromSlash(key))
	absRoot, err := filepath.Abs(s.root)
	if err != nil {
		return "", err
	}
	absFull, err := filepath.Abs(full)
	if err != nil {
		return "", err
	}
	if absFull != absRoot && !strings.HasPrefix(absFull, absRoot+string(os.PathSeparator)) {
		return "", fmt.Errorf("invalid blob key")
	}
	return absFull, nil
}
