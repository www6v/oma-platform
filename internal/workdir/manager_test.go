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
	m := workdir.NewManager(base)
	p, err := m.Ensure(context.Background(), "sess_test")
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
