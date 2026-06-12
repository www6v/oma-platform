package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestModelsListPostRequiresAPIKey(t *testing.T) {
	handler := testRouter(t)
	body := `{"provider":"ant"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/models/list",
		bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	errObj := resp["error"].(map[string]any)
	if errObj["message"] != "api_key is required" {
		t.Fatalf("message=%v", errObj["message"])
	}
}

func TestModelsListPostUnknownProviderEmpty(t *testing.T) {
	handler := testRouter(t)
	body := `{"provider":"unknown","api_key":"some-key"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/models/list",
		bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data []any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 0 {
		t.Fatalf("data=%v", resp.Data)
	}
}
