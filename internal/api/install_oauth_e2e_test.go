package api_test

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/open-ma/oma-building/internal/api"
	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/store"
)

const (
	testGitHubAppID          = "12345"
	testGitHubInstallationID = "99988"
)

func testRouterWithOAuthMocks(
	t *testing.T,
	pemKey string,
) http.Handler {
	t.Helper()
	t.Setenv("INTEGRATIONS_GATEWAY_ORIGIN", "http://test")

	ghMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/app":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":   12345,
				"slug": "test-bot",
				"bot":  map[string]string{"login": "test-bot[bot]"},
			})
		case r.Method == http.MethodPost &&
			strings.HasPrefix(r.URL.Path, "/app-manifests/") &&
			strings.HasSuffix(r.URL.Path, "/conversions"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":             12345,
				"slug":           "test-bot",
				"client_id":      "gh_client",
				"client_secret":  "gh_secret",
				"webhook_secret": "whsec_manifest",
				"pem":            pemKey,
			})
		case r.Method == http.MethodPost &&
			strings.Contains(r.URL.Path, "/access_tokens"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"token":"ghs_mock_install_token"}`))
		case r.Method == http.MethodGet &&
			strings.HasPrefix(r.URL.Path, "/app/installations/"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": 99988,
				"account": map[string]string{
					"login": "acme-corp",
					"type":  "Organization",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(ghMock.Close)

	slackMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/oauth.v2.access" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":           true,
			"access_token": "xoxb-slack-bot-token",
			"scope":        "chat:write",
			"bot_user_id":  "B123TEST",
			"app_id":       "A123TEST",
			"team": map[string]string{
				"id":   "T123TEST",
				"name": "Acme Workspace",
			},
			"authed_user": map[string]string{
				"access_token": "xoxp-slack-user-token",
				"scope":        "identity.basic",
			},
		})
	}))

	t.Cleanup(slackMock.Close)

	ghURL, err := url.Parse(ghMock.URL)
	if err != nil {
		t.Fatal(err)
	}
	slackURL, err := url.Parse(slackMock.URL)
	if err != nil {
		t.Fatal(err)
	}

	httpClient := &http.Client{
		Transport: &hostRewriteTransport{
			hosts: map[string]*url.URL{
				"api.github.com": ghURL,
				"slack.com":      slackURL,
			},
		},
	}

	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	deps, _ := testRouterDeps(t, db, &harness.FakeClient{}, "", "")
	deps.InstallBridgeHTTP = httpClient
	return api.NewRouter(deps)
}

type hostRewriteTransport struct {
	hosts map[string]*url.URL
}

func (rt *hostRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	base, ok := rt.hosts[req.URL.Host]
	if !ok {
		return http.DefaultTransport.RoundTrip(req)
	}
	clone := req.Clone(req.Context())
	u := *req.URL
	u.Scheme = base.Scheme
	u.Host = base.Host
	clone.URL = &u
	return http.DefaultTransport.RoundTrip(clone)
}

func testRSAPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	block := &pem.Block{Type: "PRIVATE KEY", Bytes: der}
	return string(pem.EncodeToMemory(block))
}

func startInstallA1(
	t *testing.T,
	handler http.Handler,
	provider, persona string,
) (pubID, formToken string) {
	t.Helper()
	agentID, envID := integrationTestAgentEnv(t, handler)
	body, _ := json.Marshal(map[string]any{
		"agentId":       agentID,
		"environmentId": envID,
		"personaName":   persona,
		"returnUrl":     "http://localhost/console/integrations",
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/integrations/"+provider+"/start-a1",
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
	pubID, _ = shell["publicationId"].(string)
	formToken, _ = shell["formToken"].(string)
	if pubID == "" || formToken == "" {
		t.Fatalf("missing publicationId/formToken: %v", shell)
	}
	return pubID, formToken
}

func queryParam(rawURL, key string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Query().Get(key)
}

func assertPublicationLive(
	t *testing.T,
	handler http.Handler,
	provider, pubID string,
) {
	t.Helper()
	req := httptest.NewRequest(
		http.MethodGet,
		"/v1/integrations/"+provider+"/publications/"+pubID,
		nil,
	)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get publication status=%d body=%s", rec.Code, rec.Body.String())
	}
	var pub map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &pub); err != nil {
		t.Fatal(err)
	}
	if pub["status"] != "live" {
		t.Fatalf("publication status=%v want live", pub["status"])
	}
	instID := pub["installation_id"]
	if instID == nil || instID == "" {
		t.Fatalf("expected installation_id on live publication: %v", pub)
	}
}

func TestInstallOAuthE2E_GitHub(t *testing.T) {
	pemKey := testRSAPrivateKeyPEM(t)
	handler := testRouterWithOAuthMocks(t, pemKey)
	pubID, formToken := startInstallA1(t, handler, "github", "OAuth E2E Bot")

	credBody, _ := json.Marshal(map[string]any{
		"formToken":     formToken,
		"appId":         testGitHubAppID,
		"privateKey":    pemKey,
		"webhookSecret": "whsec_e2e",
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/github/publications/credentials",
		bytes.NewReader(credBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("credentials status=%d body=%s", rec.Code, rec.Body.String())
	}
	var credResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &credResp); err != nil {
		t.Fatal(err)
	}
	installURL, _ := credResp["url"].(string)
	state := queryParam(installURL, "state")
	if state == "" {
		t.Fatalf("missing state in install url: %s", installURL)
	}

	callback := fmt.Sprintf(
		"/github/oauth/pub/%s/callback?installation_id=%s&state=%s&setup_action=install",
		pubID, testGitHubInstallationID, url.QueryEscape(state),
	)
	req = httptest.NewRequest(http.MethodGet, callback, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("oauth callback status=%d body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "install=ok") {
		t.Fatalf("redirect missing install=ok: %s", loc)
	}
	if !strings.Contains(loc, "publication_id="+pubID) {
		t.Fatalf("redirect missing publication_id: %s", loc)
	}

	assertPublicationLive(t, handler, "github", pubID)
}

func TestInstallOAuthE2E_Slack(t *testing.T) {
	pemKey := testRSAPrivateKeyPEM(t)
	handler := testRouterWithOAuthMocks(t, pemKey)
	pubID, formToken := startInstallA1(t, handler, "slack", "Slack OAuth Bot")

	credBody, _ := json.Marshal(map[string]any{
		"formToken":     formToken,
		"clientId":      "slack_client_e2e",
		"clientSecret":  "slack_secret_e2e",
		"signingSecret": "slack_sign_e2e",
	})
	req := httptest.NewRequest(
		http.MethodPost,
		"/slack/publications/credentials",
		bytes.NewReader(credBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("credentials status=%d body=%s", rec.Code, rec.Body.String())
	}
	var credResp map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &credResp); err != nil {
		t.Fatal(err)
	}
	authURL, _ := credResp["url"].(string)
	state := queryParam(authURL, "state")
	if state == "" {
		t.Fatalf("missing state in authorize url: %s", authURL)
	}

	callback := fmt.Sprintf(
		"/slack/oauth/pub/%s/callback?code=slack_auth_code_e2e&state=%s",
		pubID, url.QueryEscape(state),
	)
	req = httptest.NewRequest(http.MethodGet, callback, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("oauth callback status=%d body=%s", rec.Code, rec.Body.String())
	}
	loc := rec.Header().Get("Location")
	if !strings.Contains(loc, "install=ok") {
		t.Fatalf("redirect missing install=ok: %s", loc)
	}

	assertPublicationLive(t, handler, "slack", pubID)
}

func TestInstallOAuthE2E_GitHubManifest(t *testing.T) {
	pemKey := testRSAPrivateKeyPEM(t)
	handler := testRouterWithOAuthMocks(t, pemKey)
	pubID, formToken := startInstallA1(t, handler, "github", "Manifest OAuth Bot")

	req := httptest.NewRequest(
		http.MethodGet,
		"/github/manifest/start/"+formToken,
		nil,
	)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("manifest start status=%d body=%s", rec.Code, rec.Body.String())
	}
	html := rec.Body.String()
	state := extractHTMLInputValue(html, "state")
	if state == "" {
		t.Fatal("manifest page missing state input")
	}

	req = httptest.NewRequest(
		http.MethodGet,
		"/github/manifest/callback?code=manifest_code_e2e&state="+url.QueryEscape(state),
		nil,
	)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("manifest callback status=%d body=%s", rec.Code, rec.Body.String())
	}
	installURL := rec.Header().Get("Location")
	installState := queryParam(installURL, "state")
	if installState == "" {
		t.Fatalf("manifest redirect missing state: %s", installURL)
	}

	callback := fmt.Sprintf(
		"/github/oauth/pub/%s/callback?installation_id=%s&state=%s&setup_action=install",
		pubID, testGitHubInstallationID, url.QueryEscape(installState),
	)
	req = httptest.NewRequest(http.MethodGet, callback, nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("oauth callback status=%d body=%s", rec.Code, rec.Body.String())
	}

	assertPublicationLive(t, handler, "github", pubID)
}

func extractHTMLInputValue(html, name string) string {
	marker := `name="` + name + `" value="`
	idx := strings.Index(html, marker)
	if idx < 0 {
		return ""
	}
	rest := html[idx+len(marker):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		return ""
	}
	return rest[:end]
}
