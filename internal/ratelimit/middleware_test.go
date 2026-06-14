package ratelimit

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/open-ma/oma-building/internal/auth"
)

func TestMiddlewareV1WriteLimit(t *testing.T) {
	gates := NewGates(Limits{APIUserWrite: 2, APIUserRead: 100})
	called := 0
	handler := Middleware(gates, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/agents", nil)
	req.Header.Set("x-api-key", "test-key-long-enough")
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d status=%d", i, rec.Code)
		}
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", rec.Code)
	}
	if called != 2 {
		t.Fatalf("expected 2 handler calls, got %d", called)
	}
}

func TestMiddlewareBypassWhenAuthDisabled(t *testing.T) {
	gates := NewGates(Limits{APIUserWrite: 1})
	handler := Middleware(gates, true)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodPost, "/v1/agents", nil)
	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected bypass, status=%d", rec.Code)
		}
	}
}

func TestMiddlewareAuthSendIPLimit(t *testing.T) {
	gates := NewGates(Limits{
		AuthIP:        100,
		AuthSendIP:    2,
		AuthSendEmail: 100,
	})
	handler := Middleware(gates, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	body := `{"email":"user@example.com"}`
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(
			http.MethodPost,
			"/auth/sign-in/email",
			io.NopCloser(strings.NewReader(body)),
		)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d status=%d", i, rec.Code)
		}
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/auth/sign-in/email",
		io.NopCloser(strings.NewReader(body)),
	)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("expected IP send limit 429, got %d", rec.Code)
	}
}

func TestMiddlewareAuthSendEmailLimit(t *testing.T) {
	gates := NewGates(Limits{
		AuthIP:        100,
		AuthSendIP:    100,
		AuthSendEmail: 1,
	})
	handler := Middleware(gates, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	body := `{"email":"user@example.com"}`
	req := httptest.NewRequest(
		http.MethodPost,
		"/auth/sign-in/email",
		io.NopCloser(strings.NewReader(body)),
	)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first request status=%d", rec.Code)
	}
	req2 := httptest.NewRequest(
		http.MethodPost,
		"/auth/sign-in/email",
		io.NopCloser(strings.NewReader(body)),
	)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Fatalf("expected email throttle 429, got %d", rec2.Code)
	}
}

func TestMiddlewareSkipsInternalV1(t *testing.T) {
	gates := NewGates(Limits{APIUserRead: 0})
	handler := Middleware(gates, false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/internal/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("internal path should skip limit, got %d", rec.Code)
	}
}

func TestAPIPrincipalUserContext(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/v1/agents", nil)
	ctx := auth.WithUser(req.Context(), auth.User{ID: "user-1"})
	req = req.WithContext(ctx)
	if got := apiPrincipal(req); got != "user:user-1" {
		t.Fatalf("principal=%s", got)
	}
}
