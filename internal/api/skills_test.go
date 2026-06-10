package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSkillsBuiltinAndCustom(t *testing.T) {
	handler := testRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/v1/skills", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list skills status=%d", rec.Code)
	}
	var listResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	data, _ := listResp["data"].([]any)
	if len(data) < 4 {
		t.Fatalf("expected at least 4 builtin skills, got %d", len(data))
	}

	req = httptest.NewRequest(
		http.MethodGet, "/v1/skills/builtin_pdf", nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get builtin skill status=%d", rec.Code)
	}
	var builtin map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &builtin); err != nil {
		t.Fatal(err)
	}
	if builtin["source"] != "anthropic" {
		t.Fatalf("expected anthropic source, got %v", builtin["source"])
	}

	createBody := `{
		"name": "demo-skill",
		"display_title": "Demo Skill",
		"description": "A test skill",
		"files": [
			{
				"filename": "SKILL.md",
				"content": "---\nname: demo-skill\ndescription: From frontmatter\n---\n# Demo"
			}
		]
	}`
	req = httptest.NewRequest(
		http.MethodPost, "/v1/skills",
		bytes.NewBufferString(createBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create skill status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	skillID, _ := created["id"].(string)
	if skillID == "" {
		t.Fatal("missing skill id")
	}

	req = httptest.NewRequest(
		http.MethodGet,
		"/v1/skills/"+skillID+"/versions",
		nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list versions status=%d", rec.Code)
	}

	req = httptest.NewRequest(
		http.MethodDelete,
		"/v1/skills/builtin_pdf",
		nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("delete builtin status=%d", rec.Code)
	}

	req = httptest.NewRequest(
		http.MethodDelete,
		"/v1/skills/"+skillID,
		nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete skill status=%d", rec.Code)
	}
}
