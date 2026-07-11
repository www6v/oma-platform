package sandbox

import (
	"context"
	"time"
)

// Executor runs commands in an isolated sandbox (local workdir or remote VM).
type Executor interface {
	Provider() string
	Exec(ctx context.Context, command string, timeout time.Duration) (string, error)
	ReadFile(ctx context.Context, path string) ([]byte, error)
	Destroy(ctx context.Context) error
}
