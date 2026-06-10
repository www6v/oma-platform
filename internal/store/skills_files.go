package store

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SkillFileInput is one file in a skill version upload.
type SkillFileInput struct {
	Filename string `json:"filename"`
	Content  string `json:"content"`
	Encoding string `json:"encoding,omitempty"`
}

// SkillFileEntry is a manifest row for a stored skill file.
type SkillFileEntry struct {
	Filename  string `json:"filename"`
	SizeBytes int    `json:"size_bytes"`
	Encoding  string `json:"encoding"`
}

// SkillFileStore persists skill file bytes on local disk.
type SkillFileStore struct {
	baseDir string
}

// NewSkillFileStore returns a filesystem-backed skill blob store.
func NewSkillFileStore(baseDir string) *SkillFileStore {
	return &SkillFileStore{baseDir: baseDir}
}

// WriteVersionFiles stores all files for a skill version.
func (s *SkillFileStore) WriteVersionFiles(
	tenantID, skillID, version string,
	files []SkillFileInput,
) ([]SkillFileEntry, error) {
	manifest := make([]SkillFileEntry, 0, len(files))
	for _, file := range files {
		if file.Filename == "" {
			return nil, fmt.Errorf("each file must have a filename")
		}
		bytes, enc, err := skillFileBytes(file)
		if err != nil {
			return nil, err
		}
		path, err := s.filePath(tenantID, skillID, version, file.Filename)
		if err != nil {
			return nil, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(path, bytes, 0o644); err != nil {
			return nil, err
		}
		manifest = append(manifest, SkillFileEntry{
			Filename:  file.Filename,
			SizeBytes: len(bytes),
			Encoding:  enc,
		})
	}
	return manifest, nil
}

// ReadVersionFiles loads file contents for a version manifest.
func (s *SkillFileStore) ReadVersionFiles(
	tenantID, skillID, version string,
	manifest []SkillFileEntry,
) ([]map[string]string, error) {
	out := make([]map[string]string, 0, len(manifest))
	for _, entry := range manifest {
		path, err := s.filePath(tenantID, skillID, version, entry.Filename)
		if err != nil {
			return nil, err
		}
		bytes, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		enc := entry.Encoding
		if enc == "" {
			enc = "utf8"
		}
		content := string(bytes)
		if enc == "base64" {
			content = base64.StdEncoding.EncodeToString(bytes)
		}
		out = append(out, map[string]string{
			"filename": entry.Filename,
			"content":  content,
			"encoding": enc,
		})
	}
	return out, nil
}

// DeleteVersionFiles removes all files for a version.
func (s *SkillFileStore) DeleteVersionFiles(
	tenantID, skillID, version string,
	manifest []SkillFileEntry,
) error {
	for _, entry := range manifest {
		path, err := s.filePath(tenantID, skillID, version, entry.Filename)
		if err != nil {
			return err
		}
		_ = os.Remove(path)
	}
	dir := filepath.Join(s.baseDir, tenantOrDefault(tenantID), skillID, version)
	_ = os.Remove(dir)
	return nil
}

// DeleteSkillDir removes all files for a skill.
func (s *SkillFileStore) DeleteSkillDir(tenantID, skillID string) error {
	dir := filepath.Join(s.baseDir, tenantOrDefault(tenantID), skillID)
	return os.RemoveAll(dir)
}

func (s *SkillFileStore) filePath(
	tenantID, skillID, version, filename string,
) (string, error) {
	clean := filepath.Clean(filename)
	if clean == "." || strings.HasPrefix(clean, "..") ||
		strings.Contains(clean, string(os.PathSeparator)+"..") {
		return "", fmt.Errorf("invalid filename")
	}
	return filepath.Join(
		s.baseDir,
		tenantOrDefault(tenantID),
		skillID,
		version,
		clean,
	), nil
}

func skillFileBytes(file SkillFileInput) ([]byte, string, error) {
	if file.Encoding == "base64" {
		raw, err := base64.StdEncoding.DecodeString(file.Content)
		if err != nil {
			return nil, "", fmt.Errorf("invalid base64 for %s", file.Filename)
		}
		return raw, "base64", nil
	}
	return []byte(file.Content), "utf8", nil
}
