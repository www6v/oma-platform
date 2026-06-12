package api_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

func githubWebhookSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func slackWebhookSignature(secret string, body []byte, ts int64) string {
	base := fmt.Sprintf("v0:%d:%s", ts, string(body))
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(base))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func seedGitHubLivePublication(t *testing.T, handler http.Handler) (pubID string) {
	t.Helper()
	pubID = seedProviderLivePublication(
		t, handler, "github", "webhook bot", "gh_wh_smoke",
		map[string]any{
			"webhookSecret": "gh_wh_smoke",
		},
		"/v1/internal/github/publications/",
	)
	return pubID
}

func seedSlackLivePublication(t *testing.T, handler http.Handler) (pubID string) {
	t.Helper()
	pubID = seedProviderLivePublication(
		t, handler, "slack", "Slack Bot", "sl_sign_smoke",
		map[string]any{
			"signingSecret": "sl_sign_smoke",
		},
		"/v1/internal/slack/publications/",
	)
	return pubID
}

func seedProviderLivePublication(
	t *testing.T,
	handler http.Handler,
	provider, personaName, secretKey string,
	credBody map[string]any,
	bindPrefix string,
) string {
	t.Helper()
	createAgent := `{"name":"` + provider + `-webhook-agent","model":"claude-sonnet-4-20250514"}`
	req := httptest.NewRequest(
		http.MethodPost, "/v1/agents",
		bytes.NewBufferString(createAgent),
	)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("agent create status=%d", rec.Code)
	}
	var agent map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &agent)
	agentID := agent["id"].(string)

	req = httptest.NewRequest(http.MethodGet, "/v1/environments?limit=5", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var envs map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &envs)
	envID := envs["data"].([]any)[0].(map[string]any)["id"].(string)

	pubBody, _ := json.Marshal(map[string]any{
		"agentId":       agentID,
		"environmentId": envID,
		"personaName":   personaName,
		"returnUrl":     "http://localhost/console",
	})
	req = httptest.NewRequest(
		http.MethodPost,
		"/v1/integrations/"+provider+"/publications",
		bytes.NewReader(pubBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("publication status=%d body=%s", rec.Code, rec.Body.String())
	}
	var shell map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &shell)
	pubID := shell["publication_id"].(string)
	expectedWebhook := "http://test/" + provider + "/webhook/pub/" + pubID
	if shell["webhook_url"] != expectedWebhook {
		t.Fatalf("webhook_url=%v want %s", shell["webhook_url"], expectedWebhook)
	}

	credBody["returnUrl"] = "http://localhost/console"
	credJSON, _ := json.Marshal(credBody)
	req = httptest.NewRequest(
		http.MethodPatch,
		"/v1/integrations/"+provider+"/publications/"+pubID+"/credentials",
		bytes.NewReader(credJSON),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("credentials status=%d body=%s", rec.Code, rec.Body.String())
	}
	_ = secretKey

	bindBody := []byte(`{"workspace_id":"org_mock","bot_user_id":"bot_mock"}`)
	req = httptest.NewRequest(
		http.MethodPost,
		bindPrefix+pubID+"/bind-mock-install",
		bytes.NewReader(bindBody),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-internal-secret", "test-internal-secret")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("bind mock status=%d body=%s", rec.Code, rec.Body.String())
	}
	return pubID
}

func TestGitHubWebhookCreatesSession(t *testing.T) {
	t.Setenv("INTEGRATIONS_GATEWAY_ORIGIN", "http://test")
	handler := testRouter(t)
	pubID := seedGitHubLivePublication(t, handler)

	payload := []byte(`{
		"action": "labeled",
		"repository": {"full_name": "acme/demo"},
		"issue": {
			"number": 42,
			"title": "Fix auth",
			"body": "Please help",
			"html_url": "https://github.com/acme/demo/issues/42",
			"labels": [{"name": "webhook bot"}]
		},
		"label": {"name": "webhook bot"},
		"sender": {"login": "alice"}
	}`)
	const webhookSecret = "gh_wh_smoke"
	sig := githubWebhookSignature(webhookSecret, payload)

	req := httptest.NewRequest(
		http.MethodPost,
		"/github/webhook/pub/"+pubID,
		bytes.NewReader(payload),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Event", "issues")
	req.Header.Set("X-GitHub-Delivery", "del_gh_gateway_test")
	req.Header.Set("X-Hub-Signature-256", sig)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("webhook status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertWebhookSessionCreated(t, handler, rec.Body.Bytes())
}

func TestSlackWebhookCreatesSession(t *testing.T) {
	t.Setenv("INTEGRATIONS_GATEWAY_ORIGIN", "http://test")
	handler := testRouter(t)
	pubID := seedSlackLivePublication(t, handler)

	payload := []byte(`{
		"type": "event_callback",
		"event_id": "Ev_slack_gateway_test",
		"team_id": "T123",
		"event": {
			"type": "app_mention",
			"user": "U123",
			"text": "<@B123> hello from smoke",
			"channel": "C123",
			"ts": "1710000000.000100"
		}
	}`)
	const signingSecret = "sl_sign_smoke"
	ts := time.Now().Unix()
	sig := slackWebhookSignature(signingSecret, payload, ts)

	req := httptest.NewRequest(
		http.MethodPost,
		"/slack/webhook/pub/"+pubID,
		bytes.NewReader(payload),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Slack-Request-Timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("X-Slack-Signature", sig)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("webhook status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertWebhookSessionCreated(t, handler, rec.Body.Bytes())
}

func assertWebhookSessionCreated(
	t *testing.T,
	handler http.Handler,
	respBody []byte,
) {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(respBody, &resp); err != nil {
		t.Fatal(err)
	}
	if resp["ok"] != true {
		t.Fatalf("ok=%v reason=%v", resp["ok"], resp["reason"])
	}
	sessionID, ok := resp["session_id"].(string)
	if !ok || sessionID == "" {
		t.Fatalf("missing session_id: %v", resp)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		req := httptest.NewRequest(
			http.MethodGet,
			"/v1/sessions/"+sessionID+"/events",
			nil,
		)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("events status=%d body=%s", rec.Code, rec.Body.String())
		}
		var events map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &events)
		data, _ := events["data"].([]any)
		if len(data) > 0 {
			first := data[0].(map[string]any)
			if first["type"] == "user.message" {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("expected user.message session event")
}
