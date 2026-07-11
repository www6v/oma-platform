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

func TestBoxRunExecutorRoundTrip(t *testing.T) {
	t.Parallel()
	boxID := "box-abc"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/boxes"):
			_ = json.NewEncoder(w).Encode(map[string]string{"box_id": boxID})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/exec"):
			_ = json.NewEncoder(w).Encode(map[string]string{
				"execution_id": "exec-1",
			})
		case r.Method == http.MethodGet &&
			strings.Contains(r.URL.Path, "/output"):
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte(
				"event: stdout\ndata: {\"data\":\"b2s=\"}\n\n" +
					"event: exit\ndata: {\"exit_code\":0}\n\n",
			))
		case r.Method == http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := sandbox.Config{
		Provider:     sandbox.ProviderBoxRun,
		BoxRunURL:    srv.URL,
		SandboxImage: "node:22-slim",
	}
	ex, err := sandbox.NewBoxRunExecutor(
		context.Background(),
		cfg,
		"sess-1",
		srv.Client(),
	)
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

func TestValidateLiteboxAndBoxrun(t *testing.T) {
	t.Parallel()
	lite := sandbox.Config{Provider: sandbox.ProviderLiteBox}
	if err := lite.Validate(); err != nil {
		t.Fatalf("litebox validate: %v", err)
	}
	boxrun := sandbox.Config{Provider: sandbox.ProviderBoxRun}
	if err := boxrun.Validate(); err == nil {
		t.Fatal("expected boxrun missing URL error")
	}
	boxrun.BoxRunURL = "http://127.0.0.1:8100/v1/default"
	if err := boxrun.Validate(); err != nil {
		t.Fatalf("boxrun validate: %v", err)
	}
}

func TestNormalizeProviderAliases(t *testing.T) {
	t.Setenv("SANDBOX_PROVIDER", "boxlite")
	cfg := sandbox.LoadConfigFromEnv()
	if cfg.Provider != sandbox.ProviderLiteBox {
		t.Fatalf("provider=%q want litebox", cfg.Provider)
	}
	if !cfg.IsRemote() {
		t.Fatal("litebox should be remote")
	}
}
