package workdir_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/open-ma/oma-building/internal/workdir"
)

func TestEnsureWorkdirCreatesSessionDir(t *testing.T) {
	base := t.TempDir()
	m := workdir.NewManager(base, "")
	p, err := m.Ensure(context.Background(), "default", "sess_test")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p, base) {
		t.Fatalf("path=%s", p)
	}
	info, err := os.Stat(p)
	if err != nil || !info.IsDir() {
		t.Fatalf("stat: %v", err)
	}
	if filepath.Base(p) != "sess_test" {
		t.Fatalf("base=%s", filepath.Base(p))
	}
}

func TestEnsureMountsSessionOutputs(t *testing.T) {
	base := t.TempDir()
	outputsRoot := t.TempDir()
	m := workdir.NewManager(base, outputsRoot)
	p, err := m.Ensure(context.Background(), "default", "sess_out")
	if err != nil {
		t.Fatal(err)
	}

	targetDir := filepath.Join(outputsRoot, "default", "sess_out")
	info, err := os.Stat(targetDir)
	if err != nil || !info.IsDir() {
		t.Fatalf("target dir: %v", err)
	}

	link := filepath.Join(p, ".mnt", "session", "outputs")
	linkInfo, err := os.Lstat(link)
	if err != nil {
		t.Fatalf("workdir outputs link: %v", err)
	}
	if linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("expected symlink at %s", link)
	}
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		t.Fatal(err)
	}
	absTarget, err := filepath.Abs(targetDir)
	if err != nil {
		t.Fatal(err)
	}
	if resolved != absTarget {
		t.Fatalf("link=%s target=%s", resolved, absTarget)
	}
}
