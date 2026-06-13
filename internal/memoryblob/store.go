package memoryblob

import (
	"path/filepath"

	"github.com/open-ma/oma-building/internal/fileblob"
)

// Store persists memory content bytes on local disk.
type Store struct {
	files *fileblob.Store
}

// NewStore returns a filesystem-backed memory blob store.
func NewStore(root string) *Store {
	return &Store{files: fileblob.NewStore(root)}
}

// Key returns the blob key for a memory row.
func Key(tenantID, storeID, memoryID string) string {
	tenant := tenantID
	if tenant == "" {
		tenant = "default"
	}
	return filepath.Join("t", tenant, "memory", storeID, memoryID)
}

// Write stores memory bytes and returns the blob key.
func (s *Store) Write(
	tenantID, storeID, memoryID string,
	data []byte,
) (string, error) {
	key := Key(tenantID, storeID, memoryID)
	if err := s.files.WriteKey(key, data); err != nil {
		return "", err
	}
	return key, nil
}

// Read loads memory bytes by blob key.
func (s *Store) Read(key string) ([]byte, error) {
	return s.files.ReadKey(key)
}

// Delete removes a memory blob by key.
func (s *Store) Delete(key string) error {
	return s.files.DeleteKey(key)
}
