package api_test

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFileUploadJSON(t *testing.T) {
	handler := testRouter(t)

	body := `{"filename":"data.csv","content":"col1,col2\n1,2","media_type":"text/csv"}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/files",
		bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var file map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &file); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(file["id"].(string), "file-") {
		t.Fatalf("id=%v", file["id"])
	}
	if file["filename"] != "data.csv" {
		t.Fatalf("filename=%v", file["filename"])
	}
	if file["media_type"] != "text/csv" {
		t.Fatalf("media_type=%v", file["media_type"])
	}
	if file["size_bytes"].(float64) != float64(len("col1,col2\n1,2")) {
		t.Fatalf("size_bytes=%v", file["size_bytes"])
	}
}

func TestFileUploadMultipart(t *testing.T) {
	handler := testRouter(t)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, err := writer.CreateFormFile("file", "photo.png")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte{0x89, 0x50, 0x4e, 0x47}); err != nil {
		t.Fatal(err)
	}
	_ = writer.WriteField("scope_id", "sess_upload_test")
	_ = writer.WriteField("downloadable", "true")
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/files", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var file map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &file); err != nil {
		t.Fatal(err)
	}
	if file["filename"] != "photo.png" {
		t.Fatalf("filename=%v", file["filename"])
	}
	if file["scope_id"] != "sess_upload_test" {
		t.Fatalf("scope_id=%v", file["scope_id"])
	}
	if file["downloadable"] != true {
		t.Fatalf("downloadable=%v", file["downloadable"])
	}
}

func TestFileDownloadAndDelete(t *testing.T) {
	handler := testRouter(t)

	createBody := `{"filename":"hello.txt","content":"Hello, world!","media_type":"text/plain","downloadable":true}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/files",
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
	fileID := created["id"].(string)

	req = httptest.NewRequest(http.MethodGet, "/v1/files/"+fileID+"/content", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("content status=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain" {
		t.Fatalf("content-type=%q", got)
	}
	text, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatal(err)
	}
	if string(text) != "Hello, world!" {
		t.Fatalf("content=%q", text)
	}

	req = httptest.NewRequest(http.MethodDelete, "/v1/files/"+fileID, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/files/"+fileID, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete status=%d", rec.Code)
	}
}

func TestFileContentNotDownloadable(t *testing.T) {
	handler := testRouter(t)

	createBody := `{"filename":"secret.bin","content":"hidden","encoding":"utf8"}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/files",
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
	fileID := created["id"].(string)

	req = httptest.NewRequest(http.MethodGet, "/v1/files/"+fileID+"/content", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("content status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestFileUploadRejectsMissingContent(t *testing.T) {
	handler := testRouter(t)

	req := httptest.NewRequest(
		http.MethodPost, "/v1/files",
		bytes.NewBufferString(`{"filename":"empty.txt"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d", rec.Code)
	}
}

func TestFileListWithScope(t *testing.T) {
	handler := testRouter(t)

	scope := "sess_filtertest"
	createBody := `{"filename":"scoped.txt","content":"scoped","media_type":"text/plain","scope_id":"` + scope + `"}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/files",
		bytes.NewBufferString(createBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(
		http.MethodGet, "/v1/files?scope_id="+scope, nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d", rec.Code)
	}
	var list map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	data, ok := list["data"].([]any)
	if !ok || len(data) < 1 {
		t.Fatalf("data=%v", list["data"])
	}
}
