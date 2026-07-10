package workdir_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-ma/oma-building/internal/workdir"
)

func TestNormalizeSandboxPathMemory(t *testing.T) {
	t.Parallel()
	got := workdir.NormalizeSandboxPath(
		"/mnt/memory/user-preferences/prefs.md",
	)
	want := ".mnt/memory/user-preferences/prefs.md"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveSandboxPathWorkspace(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	sessionDir := filepath.Join(base, "sess1")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(sessionDir, "greeting.txt")
	if err := os.WriteFile(target, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	abs, err := workdir.ResolveSandboxPath(
		sessionDir,
		"/workspace/greeting.txt",
	)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.EvalSymlinks(target)
	if err != nil {
		t.Fatal(err)
	}
	if abs != want {
		t.Fatalf("abs=%s want=%s", abs, want)
	}
}

func TestEnsureMountsMemoryStore(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	memoryRoot := t.TempDir()
	storeID := "mstore_test"
	storeName := "user-preferences"
	targetDir := filepath.Join(memoryRoot, storeID)
	if err := os.MkdirAll(
		filepath.Join(targetDir, "preferences"),
		0o755,
	); err != nil {
		t.Fatal(err)
	}
	prefs := filepath.Join(targetDir, "preferences", "formatting.md")
	if err := os.WriteFile(prefs, []byte("bullets"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := workdir.NewManager(base, "", memoryRoot)
	p, err := m.Ensure(context.Background(), "default", "sess_mem", []workdir.MemoryMount{
		{StoreID: storeID, StoreName: storeName, ReadOnly: false},
	})
	if err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(p, ".mnt", "memory", storeName, "preferences", "formatting.md")
	data, err := os.ReadFile(link)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "bullets" {
		t.Fatalf("content=%q", string(data))
	}
}

func TestRemoveWorkdir(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	m := workdir.NewManager(base, "", "")
	p, err := m.Ensure(context.Background(), "default", "sess_rm", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Remove(context.Background(), "sess_rm"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); !os.IsNotExist(err) {
		t.Fatalf("workdir still exists: %v", err)
	}
}
