package workdir

import (
	"fmt"
	"os"
	"path/filepath"
)

// MemoryMount binds a memory store into the session workdir.
type MemoryMount struct {
	StoreID   string
	StoreName string
	ReadOnly  bool
}

// MemoryStoreDir returns the on-disk directory for one memory store.
func (m *Manager) MemoryStoreDir(storeID string) string {
	if m.memoryRoot == "" || storeID == "" {
		return ""
	}
	return filepath.Join(m.memoryRoot, storeID)
}

func mountMemoryStores(
	workdir, memoryRoot string,
	mounts []MemoryMount,
) error {
	if memoryRoot == "" || len(mounts) == 0 {
		return nil
	}
	mountParent := filepath.Join(workdir, ".mnt", "memory")
	if err := os.MkdirAll(mountParent, 0o755); err != nil {
		return fmt.Errorf("mkdir memory mount parent: %w", err)
	}
	aliasParent := filepath.Join(workdir, "mnt", "memory")
	if err := os.MkdirAll(aliasParent, 0o755); err != nil {
		return fmt.Errorf("mkdir mnt/memory: %w", err)
	}
	for _, mount := range mounts {
		if mount.StoreID == "" {
			continue
		}
		storeName := mount.StoreName
		if storeName == "" {
			storeName = mount.StoreID
		}
		targetDir := filepath.Join(memoryRoot, mount.StoreID)
		if err := os.MkdirAll(targetDir, 0o755); err != nil {
			return fmt.Errorf("mkdir memory store %s: %w", mount.StoreID, err)
		}
		absTarget, err := filepath.Abs(targetDir)
		if err != nil {
			return fmt.Errorf("abs memory target: %w", err)
		}
		workdirLink := filepath.Join(mountParent, storeName)
		if err := replaceSymlink(workdirLink, absTarget); err != nil {
			return fmt.Errorf("symlink %s: %w", workdirLink, err)
		}
		aliasLink := filepath.Join(aliasParent, storeName)
		if err := replaceSymlink(aliasLink, absTarget); err != nil {
			return fmt.Errorf("symlink %s: %w", aliasLink, err)
		}
		if mount.ReadOnly {
			_ = os.Chmod(absTarget, 0o555)
			_ = os.WriteFile(
				filepath.Join(absTarget, ".oma_readonly"),
				[]byte("1"),
				0o444,
			)
		} else {
			_ = os.Remove(filepath.Join(absTarget, ".oma_readonly"))
			_ = os.Chmod(absTarget, 0o755)
		}
		tryRootMemoryMount(absTarget, storeName)
	}
	return nil
}

func tryRootMemoryMount(targetDir, storeName string) {
	rootParent := "/mnt/memory"
	if !tryEnsureRootMountDir(rootParent) {
		return
	}
	rootLink := filepath.Join(rootParent, storeName)
	_ = replaceSymlink(rootLink, targetDir)
}

func tryEnsureRootMountDir(parent string) bool {
	info, err := os.Lstat(parent)
	if err == nil && info.IsDir() {
		return true
	}
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return false
	}
	return true
}
