package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/modelslist"
)

func TestModelsListPostWithMockAnthropic(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[{"id":"claude-sonnet-4-6","display_name":"Sonnet"}]}`))
	}))
	t.Cleanup(upstream.Close)

	r := chi.NewRouter()
	mountModelsListRoutes(r, modelsListDeps{
		Client: &modelslist.Client{
			HTTP:             upstream.Client(),
			AnthropicBaseURL: upstream.URL,
		},
	})

	body := `{"provider":"ant","api_key":"ant-key"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/models/list",
		bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Data []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Data) != 1 || resp.Data[0].ID != "claude-sonnet-4-6" {
		t.Fatalf("data=%+v", resp.Data)
	}
}

func TestModelsListPostUpstream502(t *testing.T) {
	t.Parallel()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(upstream.Close)

	r := chi.NewRouter()
	mountModelsListRoutes(r, modelsListDeps{
		Client: &modelslist.Client{
			HTTP:             upstream.Client(),
			AnthropicBaseURL: upstream.URL,
		},
	})

	body := `{"provider":"ant","api_key":"bad-key"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/models/list",
		bytes.NewBufferString(body),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}
