package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Phase 5: schema validation on /v1/environments create / update.
//
// These tests exercise the full HTTP path (handler → validator → store)
// to confirm the API rejects malformed configs and accepts well-formed
// ones. Unit-level validation tests live in
// internal/sandbox/resolver_test.go (TestValidateConfigJSON_*).

func TestCreateEnvironment_RejectsMalformedConfig(t *testing.T) {
	handler := testRouter(t)

	cases := []struct {
		name string
		body string
		want string // substring expected in 400 response body
	}{
		{
			name: "invalid_json",
			body: `{"name":"env","config":not-json}`,
			want: "invalid json",
		},
		{
			name: "wrong_field_type",
			body: `{"name":"env","config":{"type":"sandbox","sandbox":{"provider":"opensandbox","opensandbox":{"execd_port":"44772"}}}}`,
			want: "execd_port",
		},
		{
			name: "sandbox_missing",
			body: `{"name":"env","config":{"type":"sandbox"}}`,
			want: "sandbox",
		},
		{
			name: "provider_missing",
			body: `{"name":"env","config":{"type":"sandbox","sandbox":{}}}`,
			want: "sandbox.provider",
		},
		{
			name: "execd_port_out_of_range",
			body: `{"name":"env","config":{"type":"sandbox","sandbox":{"provider":"opensandbox","opensandbox":{"execd_port":99999}}}}`,
			want: "execd_port",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(
				http.MethodPost, "/v1/environments",
				bytes.NewBufferString(tc.body),
			)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("body=%q, want substring %q", rec.Body.String(), tc.want)
			}
		})
	}
}

func TestCreateEnvironment_AcceptsValidConfig(t *testing.T) {
	handler := testRouter(t)

	// 1. Legacy local environment (no config).
	body := `{"name":"legacy-local"}`
	req := httptest.NewRequest(http.MethodPost, "/v1/environments",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("legacy local: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 2. Explicit {"type":"local"}.
	body = `{"name":"explicit-local","config":{"type":"local"}}`
	req = httptest.NewRequest(http.MethodPost, "/v1/environments",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("explicit local: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 3. Full opensandbox config.
	body = `{
		"name":"opensandbox-slim",
		"config":{
			"type":"sandbox",
			"sandbox":{
				"provider":"opensandbox",
				"opensandbox":{
					"domain":"124.221.28.203:18090",
					"image":"python:3.12-slim",
					"use_server_proxy":true,
					"execd_port":44772,
					"timeout_seconds":3600,
					"cpu":"500m",
					"memory":"512Mi"
				}
			}
		}
	}`
	req = httptest.NewRequest(http.MethodPost, "/v1/environments",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("opensandbox: status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 4. Unknown type — tolerated (forward compat).
	body = `{"name":"future","config":{"type":"kubernetes","kubernetes":{"cluster":"x"}}}`
	req = httptest.NewRequest(http.MethodPost, "/v1/environments",
		bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("unknown type: status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestUpdateEnvironment_RejectsBadConfig(t *testing.T) {
	handler := testRouter(t)

	// First create a valid environment.
	createBody := `{"name":"to-update","config":{"type":"local"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/environments",
		bytes.NewBufferString(createBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var env map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	id := env["id"].(string)

	// Now try to update with a bad config — must be rejected.
	badPatch := `{"config":{"type":"sandbox","sandbox":{"provider":"opensandbox","opensandbox":{"execd_port":"bad"}}}}`
	req = httptest.NewRequest(http.MethodPut, "/v1/environments/"+id,
		bytes.NewBufferString(badPatch))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("update status=%d body=%s (want 400)", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "execd_port") {
		t.Fatalf("body=%q, want mention of execd_port", rec.Body.String())
	}
}

func TestUpdateEnvironment_AcceptsGoodConfig(t *testing.T) {
	handler := testRouter(t)

	createBody := `{"name":"to-update-2","config":{"type":"local"}}`
	req := httptest.NewRequest(http.MethodPost, "/v1/environments",
		bytes.NewBufferString(createBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var env map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &env)
	id := env["id"].(string)

	goodPatch := `{"config":{"type":"sandbox","sandbox":{"provider":"opensandbox","opensandbox":{"image":"python:3.12-slim"}}}}`
	req = httptest.NewRequest(http.MethodPut, "/v1/environments/"+id,
		bytes.NewBufferString(goodPatch))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s (want 200)", rec.Code, rec.Body.String())
	}
}
