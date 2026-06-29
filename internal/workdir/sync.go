package workdir

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// SyncSessionOutputs copies files from known workdir output locations into
// the session outputs store. This heals writes that landed under
// mnt/session/outputs instead of the canonical .mnt/session/outputs symlink.
func (m *Manager) SyncSessionOutputs(
	workdirPath, tenantID, sessionID string,
) error {
	if m.outputsRoot == "" || workdirPath == "" {
		return nil
	}
	targetDir := filepath.Join(
		m.outputsRoot, normalizeTenant(tenantID), sessionID,
	)
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("mkdir session outputs: %w", err)
	}
	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		return fmt.Errorf("abs session outputs: %w", err)
	}

	copied := make(map[string]struct{})
	sources := []string{
		filepath.Join(workdirPath, "mnt", "session", "outputs"),
		filepath.Join(workdirPath, ".mnt", "session", "outputs"),
		filepath.Join(workdirPath, "outputs"),
	}
	for _, src := range sources {
		if err := syncOutputDir(src, absTarget, copied); err != nil {
			return err
		}
	}
	return nil
}

func syncOutputDir(src, dst string, copied map[string]struct{}) error {
	info, err := os.Lstat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat output dir %s: %w", src, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		resolved, linkErr := filepath.EvalSymlinks(src)
		if linkErr == nil {
			absResolved, _ := filepath.Abs(resolved)
			absDst, _ := filepath.Abs(dst)
			if absResolved == absDst {
				return nil
			}
		}
	}
	if !info.IsDir() {
		return nil
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return fmt.Errorf("read output dir %s: %w", src, err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if _, done := copied[name]; done {
			continue
		}
		if err := copyOutputFile(
			filepath.Join(src, name),
			filepath.Join(dst, name),
		); err != nil {
			return err
		}
		copied[name] = struct{}{}
	}
	return nil
}

func copyOutputFile(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat output file %s: %w", src, err)
	}
	if srcInfo.IsDir() {
		return nil
	}
	dstInfo, err := os.Stat(dst)
	if err == nil && !dstInfo.IsDir() && dstInfo.Size() == srcInfo.Size() {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("stat output dest %s: %w", dst, err)
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open output file %s: %w", src, err)
	}
	defer in.Close()

	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("create output temp %s: %w", tmp, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("copy output file %s: %w", src, err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("close output temp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename output file %s: %w", dst, err)
	}
	return nil
}
