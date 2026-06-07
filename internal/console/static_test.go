package console_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/open-ma/oma-building/internal/console"
)

func TestStaticHandlerServesAssetAndSPA(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(dir, "index.html"),
		[]byte("<html>spa</html>"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "app.js"),
		[]byte("console.log('ok')"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	handler := console.NewStaticHandler(dir)

	req := httptest.NewRequest(http.MethodGet, "/app.js", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("asset status=%d", rec.Code)
	}
	if rec.Body.String() != "console.log('ok')" {
		t.Fatalf("asset body=%q", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/agents", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("spa status=%d", rec.Code)
	}
	if rec.Body.String() != "<html>spa</html>" {
		t.Fatalf("spa body=%q", rec.Body.String())
	}
}
