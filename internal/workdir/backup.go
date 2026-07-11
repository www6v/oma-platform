package workdir

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/open-ma/oma-building/internal/fileblob"
	"github.com/open-ma/oma-building/internal/store"
)

const backupBlobPrefix = "workspace-backups"

var backupSkipDirs = map[string]struct{}{
	".mnt":    {},
	"mnt":     {},
	"outputs": {},
}

// BackupService snapshots and restores session workdirs via tar + blob store.
type BackupService struct {
	repo     *store.WorkspaceBackupRepo
	blobs    *fileblob.Store
	disabled bool
}

// NewBackupService returns a workspace backup service.
func NewBackupService(
	repo *store.WorkspaceBackupRepo,
	blobs *fileblob.Store,
) *BackupService {
	disabled := os.Getenv("OMA_WORKSPACE_BACKUP_DISABLED") == "1"
	return &BackupService{
		repo:     repo,
		blobs:    blobs,
		disabled: disabled,
	}
}

// Snapshot tar's a workdir, uploads it, and records metadata. Best-effort.
func (b *BackupService) Snapshot(
	ctx context.Context,
	tenantID, environmentID, sessionID, workdirPath string,
) error {
	if b == nil || b.disabled || b.repo == nil || b.blobs == nil {
		return nil
	}
	if workdirPath == "" || sessionID == "" {
		return nil
	}
	if _, err := os.Stat(workdirPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	tarBytes, err := tarWorkdir(workdirPath)
	if err != nil {
		return err
	}
	if len(tarBytes) == 0 {
		return nil
	}
	id := "wsb_" + randomHex(8)
	blobKey := fmt.Sprintf(
		"%s/%s/%s/%s.tar",
		backupBlobPrefix,
		tenantOrDefault(tenantID),
		sessionID,
		id,
	)
	if err := b.blobs.WriteKey(blobKey, tarBytes); err != nil {
		return err
	}
	envID := environmentID
	if envID == "" {
		envID = store.DefaultEnvironmentID
	}
	return b.repo.Record(ctx, store.RecordWorkspaceBackupInput{
		TenantID:        tenantID,
		EnvironmentID:   envID,
		SourceSessionID: sessionID,
		Handle: store.WorkspaceBackupHandle{
			ID:  id,
			Dir: blobKey,
		},
	})
}

// TryRestore unpacks the latest backup when the workdir has no user files.
func (b *BackupService) TryRestore(
	ctx context.Context,
	tenantID, environmentID, sessionID, workdirPath string,
) error {
	if b == nil || b.disabled || b.repo == nil || b.blobs == nil {
		return nil
	}
	if sessionID == "" || workdirPath == "" {
		return nil
	}
	if !isWorkdirEmpty(workdirPath) {
		return nil
	}
	envID := environmentID
	if envID == "" {
		envID = store.DefaultEnvironmentID
	}
	row, err := b.repo.FindLatest(ctx, tenantID, envID, sessionID)
	if err != nil || row == nil {
		return err
	}
	blobKey := row.Handle.Dir
	if blobKey == "" {
		return nil
	}
	data, err := b.blobs.ReadKey(blobKey)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(workdirPath, 0o755); err != nil {
		return err
	}
	return untarWorkdir(workdirPath, data)
}

func tarWorkdir(root string) ([]byte, error) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		relSlash := filepath.ToSlash(rel)
		if skipBackupPath(relSlash) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&os.ModeSymlink != 0 {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}
		header.Name = relSlash
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(tw, f)
		closeErr := f.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
	if err != nil {
		return nil, err
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func untarWorkdir(root string, data []byte) error {
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := filepath.Clean(header.Name)
		if name == "." || strings.HasPrefix(name, "..") {
			continue
		}
		if skipBackupPath(filepath.ToSlash(name)) {
			continue
		}
		target := filepath.Join(root, filepath.FromSlash(name))
		absRoot, err := filepath.Abs(root)
		if err != nil {
			return err
		}
		absTarget, err := filepath.Abs(target)
		if err != nil {
			return err
		}
		if absTarget != absRoot &&
			!strings.HasPrefix(absTarget, absRoot+string(os.PathSeparator)) {
			return fmt.Errorf("invalid tar entry")
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(header.Mode))
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				_ = f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
		}
	}
}

func isWorkdirEmpty(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return true
	}
	for _, entry := range entries {
		if _, skip := backupSkipDirs[entry.Name()]; skip {
			continue
		}
		return false
	}
	return true
}

func skipBackupPath(rel string) bool {
	if rel == "" {
		return false
	}
	parts := strings.Split(rel, "/")
	if len(parts) == 0 {
		return false
	}
	_, skip := backupSkipDirs[parts[0]]
	return skip
}

func tenantOrDefault(tenantID string) string {
	if tenantID == "" {
		return "default"
	}
	return tenantID
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
