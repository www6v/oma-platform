package outbound_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/open-ma/oma-building/internal/outbound"
	"github.com/open-ma/oma-building/internal/store"
)

func TestProxyInjectsBearerToken(t *testing.T) {
	var gotAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer upstream.Close()

	ctx := context.Background()
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close(db)

	creds := store.NewCredentialRepo(db)
	_, err = creds.Create(ctx, store.CreateCredentialInput{
		TenantID:    "default",
		VaultID:     "vault-1",
		DisplayName: "upstream",
		Auth: json.RawMessage(`{
			"type": "static_bearer",
			"mcp_server_url": "` + upstream.URL + `",
			"token": "vault-secret"
		}`),
	})
	if err != nil {
		t.Fatal(err)
	}

	proxy := outbound.NewProxy(outbound.ProxyDeps{
		Resolver: &outbound.Resolver{
			Sessions: &stubSessions{
				sess: &store.Session{ID: "sess-1", TenantID: "default"},
			},
			Credentials: creds,
		},
		APIKey: "dev-key",
	})
	proxySrv := httptest.NewServer(proxy)
	defer proxySrv.Close()

	proxyURL, err := url.Parse(proxySrv.URL)
	if err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest(
		http.MethodGet,
		upstream.URL+"/secret",
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-OMA-Session-Id", "sess-1")
	req.Header.Set("Proxy-Authorization", "Bearer dev-key")

	client := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
	if gotAuth != "Bearer vault-secret" {
		t.Fatalf("Authorization=%q", gotAuth)
	}
}
