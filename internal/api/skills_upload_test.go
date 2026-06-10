package api_test

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSkillsZipUpload(t *testing.T) {
	handler := testRouter(t)
	zipBytes := buildTestSkillZip(t, "zip-skill", "From zip", true)

	body, contentType := multipartZipBody(t, zipBytes, "My Title")
	req := httptest.NewRequest(
		http.MethodPost, "/v1/skills/upload", body,
	)
	req.Header.Set("Content-Type", contentType)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", rec.Code, rec.Body.String())
	}
	var skill map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &skill); err != nil {
		t.Fatal(err)
	}
	if skill["name"] != "zip-skill" {
		t.Fatalf("name=%v", skill["name"])
	}
	if skill["display_title"] != "My Title" {
		t.Fatalf("display_title=%v", skill["display_title"])
	}
	files, _ := skill["files"].([]any)
	if len(files) < 2 {
		t.Fatalf("expected files in response, got %v", skill["files"])
	}
}

func TestSkillsVersionZipUpload(t *testing.T) {
	handler := testRouter(t)

	createBody := `{
		"name": "versioned",
		"display_title": "Versioned",
		"files": [{"filename":"SKILL.md","content":"---\nname: versioned\ndescription: v1\n---\n"}]
	}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/skills",
		bytes.NewBufferString(createBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d", rec.Code)
	}
	var created map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	skillID := created["id"].(string)
	before := created["latest_version"].(string)

	zipBytes := buildTestSkillZip(t, "versioned", "v2 from zip", true)
	body, contentType := multipartZipBody(t, zipBytes, "")
	req = httptest.NewRequest(
		http.MethodPost,
		"/v1/skills/"+skillID+"/versions/upload",
		body,
	)
	req.Header.Set("Content-Type", contentType)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("version upload status=%d body=%s", rec.Code, rec.Body.String())
	}
	var version map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &version); err != nil {
		t.Fatal(err)
	}
	if version["version"] == before {
		t.Fatalf("expected new version, still %v", before)
	}
}

func TestSessionOutputsListAndDownload(t *testing.T) {
	outputsDir := t.TempDir()
	handler := testRouterWithOutputs(t, outputsDir)

	createAgent := `{"name":"out-agent","model":"claude-sonnet-4-20250514"}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/agents",
		bytes.NewBufferString(createAgent),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("agent create status=%d", rec.Code)
	}
	var agent map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &agent)

	req = httptest.NewRequest(
		http.MethodPost, "/v1/sessions",
		bytes.NewBufferString(`{"agent":"`+agent["id"].(string)+`"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("session create status=%d", rec.Code)
	}
	var sess map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &sess)
	sid := sess["id"].(string)

	outDir := filepath.Join(outputsDir, "default", sid)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(outDir, "report.txt"),
		[]byte("hello output"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	req = httptest.NewRequest(
		http.MethodGet, "/v1/sessions/"+sid+"/outputs", nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("outputs list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var listResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	data, _ := listResp["data"].([]any)
	if len(data) != 1 {
		t.Fatalf("expected 1 output, got %v", listResp)
	}

	req = httptest.NewRequest(
		http.MethodGet,
		"/v1/sessions/"+sid+"/outputs/report.txt",
		nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("download status=%d", rec.Code)
	}
	if rec.Body.String() != "hello output" {
		t.Fatalf("body=%q", rec.Body.String())
	}

	req = httptest.NewRequest(
		http.MethodGet, "/v1/files?scope_id="+sid, nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("files list status=%d", rec.Code)
	}
	var filesResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &filesResp); err != nil {
		t.Fatal(err)
	}
	fileData, _ := filesResp["data"].([]any)
	if len(fileData) != 1 {
		t.Fatalf("expected synthesized file row, got %v", filesResp)
	}
}

func buildTestSkillZip(
	t *testing.T,
	name, description string,
	nested bool,
) []byte {
	t.Helper()
	root := ""
	if nested {
		root = name + "/"
	}
	md := "---\nname: " + name + "\ndescription: " + description + "\n---\n"
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for path, content := range map[string]string{
		root + "SKILL.md":  md,
		root + "helper.py": "print('hi')",
	} {
		w, err := zw.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(w, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func multipartZipBody(
	t *testing.T,
	zipBytes []byte,
	displayTitle string,
) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	part, err := w.CreateFormFile("file", "skill.zip")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(zipBytes); err != nil {
		t.Fatal(err)
	}
	if displayTitle != "" {
		if err := w.WriteField("display_title", displayTitle); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, w.FormDataContentType()
}
