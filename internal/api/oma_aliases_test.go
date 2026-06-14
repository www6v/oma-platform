package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOmaAliasRoutesMirrorBarePaths(t *testing.T) {
	handler := testRouter(t)

	cases := []struct {
		bare string
		oma  string
		key  string
	}{
		{"/v1/me", "/v1/oma/me", "user"},
		{"/v1/cost_report?days=30", "/v1/oma/cost_report?days=30", "type"},
		{"/v1/evals/runs", "/v1/oma/evals/runs", "data"},
		{
			"/v1/integrations/linear/installations",
			"/v1/oma/integrations/linear/installations",
			"data",
		},
		{"/v1/runtimes", "/v1/oma/runtimes", "runtimes"},
		{"/v1/model_cards", "/v1/oma/model_cards", "data"},
	}

	for _, tc := range cases {
		t.Run(tc.bare+" vs "+tc.oma, func(t *testing.T) {
			bareReq := httptest.NewRequest(http.MethodGet, tc.bare, nil)
			bareRec := httptest.NewRecorder()
			handler.ServeHTTP(bareRec, bareReq)
			if bareRec.Code != http.StatusOK {
				t.Fatalf("bare status=%d body=%s", bareRec.Code, bareRec.Body.String())
			}

			omaReq := httptest.NewRequest(http.MethodGet, tc.oma, nil)
			omaRec := httptest.NewRecorder()
			handler.ServeHTTP(omaRec, omaReq)
			if omaRec.Code != http.StatusOK {
				t.Fatalf("oma status=%d body=%s", omaRec.Code, omaRec.Body.String())
			}

			var bareBody map[string]any
			var omaBody map[string]any
			if err := json.Unmarshal(bareRec.Body.Bytes(), &bareBody); err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(omaRec.Body.Bytes(), &omaBody); err != nil {
				t.Fatal(err)
			}
			if _, ok := bareBody[tc.key]; !ok {
				t.Fatalf("bare missing %q: %v", tc.key, bareBody)
			}
			if _, ok := omaBody[tc.key]; !ok {
				t.Fatalf("oma missing %q: %v", tc.key, omaBody)
			}
		})
	}
}

func TestOmaTenantsAliasMounted(t *testing.T) {
	handler := testRouter(t)
	body := []byte(`{"name":"oma alias workspace"}`)

	req := httptest.NewRequest(
		http.MethodPost, "/v1/oma/tenants", bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("oma status=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(
		http.MethodPost, "/v1/tenants", bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("bare status=%d body=%s", rec.Code, rec.Body.String())
	}
}
