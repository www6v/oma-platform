package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSessionUpdateTitleAndMetadata(t *testing.T) {
	handler := testRouter(t)

	agentBody := `{"name":"upd-agent","model":"claude-sonnet-4-20250514"}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/agents", bytes.NewBufferString(agentBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var agent map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &agent)

	sessBody := `{"agent":"` + agent["id"].(string) + `","title":"Original"}`
	req = httptest.NewRequest(
		http.MethodPost, "/v1/sessions", bytes.NewBufferString(sessBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var sess map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &sess)
	sid := sess["id"].(string)

	updateBody := `{"title":"Updated Title","metadata":{"env":"staging"}}`
	req = httptest.NewRequest(
		http.MethodPost, "/v1/sessions/"+sid, bytes.NewBufferString(updateBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body.String())
	}
	var updated map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated["title"] != "Updated Title" {
		t.Fatalf("title=%v", updated["title"])
	}
	meta := updated["metadata"].(map[string]any)
	if meta["env"] != "staging" {
		t.Fatalf("metadata=%v", meta)
	}
}

func TestSessionResourceCRUD(t *testing.T) {
	handler := testRouter(t)

	fileBody := `{"filename":"crud.txt","content":"data","media_type":"text/plain"}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/files", bytes.NewBufferString(fileBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var uploaded map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &uploaded)
	fileID := uploaded["id"].(string)

	agentBody := `{"name":"res-crud","model":"claude-sonnet-4-20250514"}`
	req = httptest.NewRequest(
		http.MethodPost, "/v1/agents", bytes.NewBufferString(agentBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
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
	var sess map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &sess)
	sid := sess["id"].(string)

	addPayload, _ := json.Marshal(map[string]any{
		"type":       "file",
		"file_id":    fileID,
		"mount_path": "/workspace/crud.txt",
	})
	req = httptest.NewRequest(
		http.MethodPost,
		"/v1/sessions/"+sid+"/resources",
		bytes.NewReader(addPayload),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("add status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resource map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resource); err != nil {
		t.Fatal(err)
	}
	rid, _ := resource["id"].(string)
	if rid == "" {
		t.Fatalf("missing resource id: %v", resource)
	}

	req = httptest.NewRequest(
		http.MethodGet, "/v1/sessions/"+sid+"/resources/"+rid, nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status=%d", rec.Code)
	}

	req = httptest.NewRequest(
		http.MethodGet, "/v1/sessions/"+sid+"/resources", nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d", rec.Code)
	}

	req = httptest.NewRequest(
		http.MethodDelete, "/v1/sessions/"+sid+"/resources/"+rid, nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestEnvironmentDelete(t *testing.T) {
	handler := testRouter(t)

	createBody := `{"name":"del-env","config":{"type":"cloud"}}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/environments", bytes.NewBufferString(createBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var env map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	eid := env["id"].(string)

	req = httptest.NewRequest(http.MethodDelete, "/v1/environments/"+eid, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	if body["type"] != "environment_deleted" {
		t.Fatalf("type=%v", body["type"])
	}

	req = httptest.NewRequest(http.MethodGet, "/v1/environments/"+eid, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("get after delete status=%d", rec.Code)
	}
}

func TestSessionThreadRetrieveAndArchive(t *testing.T) {
	handler := testRouter(t)

	agentBody := `{"name":"thread-agent","model":"claude-sonnet-4-20250514"}`
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
	var sess map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &sess)
	sid := sess["id"].(string)

	evBody := `{"events":[{"type":"session.thread_created","session_thread_id":"sthr_worker","agent_id":"agt_w","agent_name":"Worker","parent_thread_id":"sthr_primary"}]}`
	req = httptest.NewRequest(
		http.MethodPost,
		"/v1/sessions/"+sid+"/events",
		bytes.NewBufferString(evBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("events status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(
		http.MethodGet, "/v1/sessions/"+sid+"/threads/sthr_worker", nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("retrieve status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(
		http.MethodPost, "/v1/sessions/"+sid+"/threads/sthr_worker/archive", nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("archive status=%d body=%s", rec.Code, rec.Body.String())
	}
	var archived map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &archived)
	if archived["status"] != "archived" {
		t.Fatalf("status=%v", archived["status"])
	}
}
