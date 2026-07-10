package workdir_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-ma/oma-building/internal/workdir"
)

func TestEnsureMountsMntSessionOutputsAlias(t *testing.T) {
	base := t.TempDir()
	outputsRoot := t.TempDir()
	m := workdir.NewManager(base, outputsRoot, "")
	p, err := m.Ensure(context.Background(), "default", "sess_mnt", nil)
	if err != nil {
		t.Fatal(err)
	}

	link := filepath.Join(p, "mnt", "session", "outputs")
	linkInfo, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("mnt/session/outputs link: %v", err)
	}
	if linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected symlink at %s", link)
	}

	targetDir := filepath.Join(outputsRoot, "default", "sess_mnt")
	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		t.Fatal(err)
	}
	absTarget, err = filepath.EvalSymlinks(absTarget)
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != absTarget {
		t.Fatalf("link=%s target=%s", resolved, absTarget)
	}
}

func TestSyncSessionOutputsFromOrphanDir(t *testing.T) {
	base := t.TempDir()
	outputsRoot := t.TempDir()
	m := workdir.NewManager(base, outputsRoot, "")
	p, err := m.Ensure(context.Background(), "default", "sess_sync", nil)
	if err != nil {
		t.Fatal(err)
	}

	orphanDir := filepath.Join(p, "mnt", "session", "outputs")
	if err := os.RemoveAll(orphanDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(orphanDir, 0o755); err != nil {
		t.Fatal(err)
	}
	report := filepath.Join(orphanDir, "report.html")
	if err := os.WriteFile(report, []byte("<html>ok</html>"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := m.SyncSessionOutputs(p, "default", "sess_sync"); err != nil {
		t.Fatal(err)
	}

	stored := filepath.Join(outputsRoot, "default", "sess_sync", "report.html")
	data, err := os.ReadFile(stored)
	if err != nil {
		t.Fatalf("stored report: %v", err)
	}
	if !strings.Contains(string(data), "ok") {
		t.Fatalf("content=%q", string(data))
	}
}
