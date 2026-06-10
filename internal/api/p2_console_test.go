package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestP2ConsoleStubs(t *testing.T) {
	handler := testRouter(t)

	cases := []struct {
		path string
		key  string
	}{
		{"/v1/runtimes", "runtimes"},
		{"/v1/models/list", "data"},
		{"/v1/files", "data"},
		{"/v1/memory_stores", "data"},
		{"/v1/evals/runs", "data"},
		{"/v1/integrations/linear/installations", "data"},
		{"/v1/integrations/github/publications", "data"},
		{
			"/v1/integrations/slack/agents/agt_test/publications",
			"data",
		},
	}

	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			var resp map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatal(err)
			}
			if _, ok := resp[tc.key]; !ok {
				t.Fatalf("missing %q: %v", tc.key, resp)
			}
		})
	}
}

func TestP2SessionAuxStubs(t *testing.T) {
	handler := testRouter(t)

	createAgent := `{"name":"stub-agent","model":"claude-sonnet-4-20250514"}`
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

	sessBody := `{"agent":"` + agent["id"].(string) + `"}`
	req = httptest.NewRequest(
		http.MethodPost, "/v1/sessions",
		bytes.NewBufferString(sessBody),
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

	for _, path := range []string{
		"/v1/sessions/" + sid + "/threads",
		"/v1/sessions/" + sid + "/pending",
		"/v1/sessions/" + sid + "/outputs",
		"/v1/sessions/" + sid + "/trajectory",
	} {
		t.Run(path, func(t *testing.T) {
			req = httptest.NewRequest(http.MethodGet, path, nil)
			rec = httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}

	req = httptest.NewRequest(
		http.MethodGet, "/v1/sessions/"+sid+"/trajectory", nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var traj map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &traj); err != nil {
		t.Fatal(err)
	}
	if traj["schema_version"] != "oma.trajectory.v1" {
		t.Fatalf("unexpected schema: %v", traj["schema_version"])
	}
}
