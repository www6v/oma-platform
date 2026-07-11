package sandbox_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/sandbox"
)

func TestDaytonaExecutorRoundTrip(t *testing.T) {
	t.Parallel()
	sandboxID := "sb-test"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/sandbox"):
			_ = json.NewEncoder(w).Encode(map[string]string{"id": sandboxID})
		case r.Method == http.MethodPost &&
			strings.Contains(r.URL.Path, "/process/execute"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"result":   "ok",
				"exitCode": 0,
			})
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := sandbox.Config{
		Provider:       sandbox.ProviderDaytona,
		DaytonaAPIKey:  "test-key",
		DaytonaAPIBase: srv.URL,
		DaytonaProxy:   srv.URL + "/toolbox",
		SandboxImage:   "node:22-slim",
	}
	ex, err := sandbox.NewDaytonaExecutor(context.Background(), cfg, srv.Client())
	if err != nil {
		t.Fatal(err)
	}
	out, err := ex.Exec(context.Background(), "echo ok", 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if out != "ok" {
		t.Fatalf("output=%q", out)
	}
	if err := ex.Destroy(context.Background()); err != nil {
		t.Fatal(err)
	}
}
