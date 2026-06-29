package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesListIncludesSessionOutputs(t *testing.T) {
	outputsDir := t.TempDir()
	handler := testRouterWithOutputs(t, outputsDir)

	agentBody := `{"name":"out-agent","model":"claude-sonnet-4-20250514"}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/agents", bytes.NewBufferString(agentBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var agent map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &agent); err != nil {
		t.Fatal(err)
	}

	sessBody := `{"agent":"` + agent["id"].(string) + `","title":"outputs"}`
	req = httptest.NewRequest(
		http.MethodPost, "/v1/sessions", bytes.NewBufferString(sessBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("session status=%d body=%s", rec.Code, rec.Body.String())
	}
	var sess map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &sess); err != nil {
		t.Fatal(err)
	}
	sessionID := sess["id"].(string)

	reportPath := filepath.Join(outputsDir, "default", sessionID, "report.html")
	if err := os.MkdirAll(filepath.Dir(reportPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		reportPath, []byte("<html>report</html>"), 0o644,
	); err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(
		http.MethodGet, "/v1/files?scope_id="+sessionID, nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	data, ok := body["data"].([]any)
	if !ok {
		t.Fatalf("data=%v", body["data"])
	}
	for _, item := range data {
		row, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if row["filename"] == "report.html" {
			return
		}
	}
	t.Fatalf("report.html not in files list: %v", body["data"])
}
