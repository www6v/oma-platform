package vaultoauth_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/open-ma/oma-building/internal/vaultoauth"
)

func TestRefreshMetadataOf(t *testing.T) {
	t.Parallel()

	meta, err := vaultoauth.RefreshMetadataOf(json.RawMessage(`{
		"type": "mcp_oauth",
		"refresh_token": "rt",
		"token_endpoint": "https://auth.example/token"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if meta == nil || meta.RefreshToken != "rt" {
		t.Fatalf("meta=%+v", meta)
	}

	static, err := vaultoauth.RefreshMetadataOf(json.RawMessage(`{
		"type": "static_bearer",
		"token": "x"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if static != nil {
		t.Fatal("expected nil for static_bearer")
	}
}

func TestRefreshMcpOAuth(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, "form", http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "refresh_token" {
			http.Error(w, "grant", http.StatusBadRequest)
			return
		}
		if r.Form.Get("refresh_token") != "old-refresh" {
			http.Error(w, "refresh", http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()

	expires := 3600
	got, err := vaultoauth.RefreshMcpOAuth(
		context.Background(),
		vaultoauth.RefreshMetadata{
			RefreshToken:  "old-refresh",
			TokenEndpoint: srv.URL,
		},
		srv.Client(),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got.AccessToken != "new-access" || got.RefreshToken != "new-refresh" {
		t.Fatalf("tokens=%+v", got)
	}
	if got.ExpiresIn == nil || *got.ExpiresIn != expires {
		t.Fatalf("expires_in=%v", got.ExpiresIn)
	}
}
