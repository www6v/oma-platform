package api_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-ma/oma-building/internal/api"
)

func TestConsoleStaticBypassesAuth(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dir, "index.html"),
		[]byte("<html>ok</html>"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	handler := api.NewRouter(api.Deps{
		APIKey:     "secret",
		ConsoleDir: dir,
	})

	req := httptest.NewRequest(http.MethodGet, "/agents", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%q", rec.Code, rec.Body.String())
	}
}

func TestAuthDisabledBypassesAPIKey(t *testing.T) {
	handler := api.NewRouter(api.Deps{
		APIKey:       "secret",
		AuthDisabled: true,
	})

	req := httptest.NewRequest(http.MethodGet, "/v1/agents", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatalf("auth disabled should bypass API key")
	}
}

func TestAuthDisabledAuthInfo(t *testing.T) {
	handler := api.NewRouter(api.Deps{AuthDisabled: true})

	req := httptest.NewRequest(http.MethodGet, "/auth-info", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestAuthDisabledSession(t *testing.T) {
	handler := api.NewRouter(api.Deps{AuthDisabled: true})

	req := httptest.NewRequest(http.MethodGet, "/auth/get-session", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
}
