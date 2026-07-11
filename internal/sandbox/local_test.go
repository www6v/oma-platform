package sandbox_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/sandbox"
)

func TestLocalExecutorExecAndReadFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dir, "hello.txt"),
		[]byte("hi"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	ex := sandbox.NewLocalExecutor(dir)
	out, err := ex.Exec(
		context.Background(),
		"cat hello.txt",
		5*time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	if out != "hi" {
		t.Fatalf("output=%q", out)
	}
	data, err := ex.ReadFile(context.Background(), "hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hi" {
		t.Fatalf("read=%q", data)
	}
}

func TestLocalExecutorNonZeroExit(t *testing.T) {
	t.Parallel()
	ex := sandbox.NewLocalExecutor(t.TempDir())
	out, err := ex.Exec(
		context.Background(),
		"exit 7",
		5*time.Second,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "[exit 7]") {
		t.Fatalf("output=%q", out)
	}
}
