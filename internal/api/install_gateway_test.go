package api_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestInstallGatewayManifestStart(t *testing.T) {
	handler := testRouter(t)
	agentID, envID := integrationTestAgentEnv(t, handler)

	body, _ := json.Marshal(map[string]any{
		"agentId":       agentID,
		"environmentId": envID,
		"personaName":   "Manifest Bot",
		"returnUrl":     "http://localhost/console/integrations",
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/integrations/github/start-a1",
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("start-a1 status=%d body=%s", rec.Code, rec.Body.String())
	}
	var shell map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &shell); err != nil {
		t.Fatal(err)
	}
	formToken, _ := shell["formToken"].(string)
	if formToken == "" {
		t.Fatalf("missing formToken: %v", shell)
	}

	req = httptest.NewRequest(
		http.MethodGet,
		"/github/manifest/start/"+formToken,
		nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("manifest start status=%d body=%s", rec.Code, rec.Body.String())
	}
	html := rec.Body.String()
	if !strings.Contains(html, "github.com/settings/apps/new") {
		t.Fatalf("expected manifest form action, got: %s", html[:200])
	}
	if !strings.Contains(html, "Manifest Bot") {
		t.Fatal("expected persona name in HTML")
	}
}

func TestInstallGatewayOAuthCallbacksMissingParams(t *testing.T) {
	handler := testRouter(t)
	cases := []struct {
		path string
	}{
		{"/github/oauth/pub/pub_test/callback"},
		{"/slack/oauth/pub/pub_test/callback?code=x"},
		{"/github/manifest/callback?code=x"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.path, nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestInstallGatewayPublicCredentialsInvalidToken(t *testing.T) {
	handler := testRouter(t)
	for _, path := range []string{
		"/github/publications/credentials",
		"/slack/publications/credentials",
	} {
		t.Run(path, func(t *testing.T) {
			body := bytes.NewBufferString(`{"formToken":"invalid.token.here"}`)
			req := httptest.NewRequest(http.MethodPost, path, body)
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestInstallGatewayHandoffSetupPages(t *testing.T) {
	handler := testRouter(t)
	agentID, envID := integrationTestAgentEnv(t, handler)

	startBody, _ := json.Marshal(map[string]any{
		"agentId":       agentID,
		"environmentId": envID,
		"personaName":   "Handoff Bot",
		"returnUrl":     "http://localhost/console/integrations",
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/integrations/github/start-a1",
		bytes.NewReader(startBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("start-a1 status=%d", rec.Code)
	}
	var shell map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &shell)
	formToken := shell["formToken"].(string)

	handoffBody, _ := json.Marshal(map[string]any{"formToken": formToken})
	req = httptest.NewRequest(
		http.MethodPost,
		"/v1/integrations/github/handoff-link",
		bytes.NewReader(handoffBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("handoff status=%d body=%s", rec.Code, rec.Body.String())
	}
	var handoff map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &handoff)
	url, _ := handoff["url"].(string)
	if !strings.Contains(url, "/github-setup/") {
		t.Fatalf("unexpected handoff url: %s", url)
	}
	token := strings.TrimPrefix(url, "http://127.0.0.1:8787/github-setup/")
	req = httptest.NewRequest(
		http.MethodGet, "/github-setup/"+token, nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup page status=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "Handoff Bot") {
		t.Fatal("expected persona on setup page")
	}
}
