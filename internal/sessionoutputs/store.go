package sessionoutputs

import (
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"strings"
)

// File is one agent-written output under /mnt/session/outputs/.
type File struct {
	Filename  string
	SizeBytes int64
	UploadedAt string
	MediaType string
}

// Store lists and reads session output files from local disk.
type Store struct {
	root string
}

// NewStore returns a filesystem-backed session outputs store.
func NewStore(root string) *Store {
	return &Store{root: root}
}

// List returns output files for a session, or nil when the directory is missing.
func (s *Store) List(tenantID, sessionID string) ([]File, error) {
	dir, err := s.sessionDir(tenantID, sessionID)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []File{}, nil
		}
		return nil, err
	}
	out := make([]File, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		filename := entry.Name()
		out = append(out, File{
			Filename:  filename,
			SizeBytes: info.Size(),
			UploadedAt: info.ModTime().UTC().Format(
				"2006-01-02T15:04:05.000Z",
			),
			MediaType: GuessMime(filename),
		})
	}
	return out, nil
}

// Read opens one output file. Returns os.ErrNotExist when missing.
func (s *Store) Read(
	tenantID, sessionID, filename string,
) (io.ReadCloser, int64, string, error) {
	if err := validateFilename(filename); err != nil {
		return nil, 0, "", err
	}
	dir, err := s.sessionDir(tenantID, sessionID)
	if err != nil {
		return nil, 0, "", err
	}
	full := filepath.Join(dir, filename)
	info, err := os.Stat(full)
	if err != nil {
		return nil, 0, "", err
	}
	if info.IsDir() {
		return nil, 0, "", os.ErrNotExist
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, 0, "", err
	}
	return f, info.Size(), GuessMime(filename), nil
}

func (s *Store) sessionDir(tenantID, sessionID string) (string, error) {
	if err := validateSessionID(sessionID); err != nil {
		return "", err
	}
	tenant := tenantID
	if tenant == "" {
		tenant = "default"
	}
	return filepath.Join(s.root, tenant, sessionID), nil
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

func validateFilename(filename string) error {
	if filename == "" ||
		strings.Contains(filename, "..") ||
		strings.ContainsAny(filename, `/\`) {
		return fmt.Errorf("invalid filename")
	}
	return nil
}

// GuessMime returns a content type for a session output filename.
func GuessMime(filename string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	if ext != "" {
		if mt := mime.TypeByExtension(ext); mt != "" {
			return mt
		}
	}
	switch strings.TrimPrefix(ext, ".") {
	case "md":
		return "text/markdown"
	case "htm":
		return "text/html"
	default:
		return "application/octet-stream"
	}
}
