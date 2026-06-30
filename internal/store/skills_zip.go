package store

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
)

// ZipVersionFiles packs skill version files into a zip archive.
func (s *SkillFileStore) ZipVersionFiles(
	tenantID, skillID, version string,
	manifest []SkillFileEntry,
) ([]byte, error) {
	buf := new(bytes.Buffer)
	zw := zip.NewWriter(buf)
	for _, entry := range manifest {
		path, err := s.filePath(tenantID, skillID, version, entry.Filename)
		if err != nil {
			_ = zw.Close()
			return nil, err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			_ = zw.Close()
			return nil, err
		}
		w, err := zw.Create(entry.Filename)
		if err != nil {
			_ = zw.Close()
			return nil, err
		}
		if _, err := io.Copy(w, bytes.NewReader(raw)); err != nil {
			_ = zw.Close()
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
