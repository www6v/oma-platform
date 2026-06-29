package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionCreateWithResources(t *testing.T) {
	handler := testRouter(t)

	fileBody := `{"filename":"init-file.txt","content":"initial content","media_type":"text/plain"}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/files", bytes.NewBufferString(fileBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("upload status=%d body=%s", rec.Code, rec.Body.String())
	}
	var uploaded map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &uploaded); err != nil {
		t.Fatal(err)
	}
	fileID := uploaded["id"].(string)

	agentBody := `{"name":"res-agent","model":"claude-sonnet-4-20250514"}`
	req = httptest.NewRequest(
		http.MethodPost, "/v1/agents", bytes.NewBufferString(agentBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("agent status=%d body=%s", rec.Code, rec.Body.String())
	}
	var agent map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &agent); err != nil {
		t.Fatal(err)
	}

	sessPayload, err := json.Marshal(map[string]any{
		"agent": agent["id"],
		"title": "With Resources",
		"resources": []any{
			map[string]any{
				"type":       "file",
				"file_id":    fileID,
				"mount_path": "/workspace/init-file.txt",
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	req = httptest.NewRequest(
		http.MethodPost, "/v1/sessions", bytes.NewReader(sessPayload),
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
	resources, ok := sess["resources"].([]any)
	if !ok || len(resources) != 1 {
		t.Fatalf("resources=%v", sess["resources"])
	}
	res := resources[0].(map[string]any)
	if res["type"] != "file" {
		t.Fatalf("type=%v", res["type"])
	}
	scopedID, _ := res["file_id"].(string)
	if scopedID == "" || scopedID == fileID {
		t.Fatalf("expected scoped file_id, got %q", scopedID)
	}
	if res["mount_path"] != "/workspace/init-file.txt" {
		t.Fatalf("mount_path=%v", res["mount_path"])
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/files/"+scopedID, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("scoped file status=%d body=%s", rec.Code, rec.Body.String())
	}
	var scopedFile map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &scopedFile); err != nil {
		t.Fatal(err)
	}
	if scopedFile["scope_id"] != sess["id"] {
		t.Fatalf("scope_id=%v want %v", scopedFile["scope_id"], sess["id"])
	}
}

func TestSessionCreateWithoutResources(t *testing.T) {
	handler := testRouter(t)

	agentBody := `{"name":"no-res","model":"claude-sonnet-4-20250514"}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/agents", bytes.NewBufferString(agentBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var agent map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &agent)

	sessBody := `{"agent":"` + agent["id"].(string) + `"}`
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
	resources, ok := sess["resources"].([]any)
	if !ok {
		t.Fatalf("resources=%v", sess["resources"])
	}
	if len(resources) != 0 {
		t.Fatalf("expected empty resources, got %v", resources)
	}
}
