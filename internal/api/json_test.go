package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/open-ma/oma-building/internal/api"
)

func TestAPIErrorEnvelopeUnauthorized(t *testing.T) {
	handler := api.NewRouter(api.Deps{APIKey: "secret"})
	req := httptest.NewRequest(http.MethodGet, "/v1/agents", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d", rec.Code)
	}

	var body struct {
		Type string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Type != "error" {
		t.Fatalf("type=%q", body.Type)
	}
	if body.Error.Type != "authentication_error" {
		t.Fatalf("error.type=%q", body.Error.Type)
	}
	if body.Error.Message == "" {
		t.Fatal("expected error message")
	}
	if body.RequestID == "" {
		t.Fatal("expected request_id")
	}
}

func TestAPIErrorEnvelopeBadRequest(t *testing.T) {
	handler := testRouter(t)
	req := httptest.NewRequest(
		http.MethodGet,
		"/v1/agents?status=invalid",
		nil,
	)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Type string `json:"type"`
		Error struct {
			Type string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Type != "error" || body.Error.Type != "invalid_request_error" {
		t.Fatalf("body=%s", rec.Body.String())
	}
}
