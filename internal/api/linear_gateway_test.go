package api_test

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func linearWebhookSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func seedLinearLivePublication(t *testing.T, handler http.Handler) (pubID string) {
	t.Helper()
	createAgent := `{"name":"linear-webhook-agent","model":"claude-sonnet-4-20250514"}`
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
		"personaName":   "Webhook Bot",
		"returnUrl":     "http://localhost/console",
	})
	req = httptest.NewRequest(
		http.MethodPost,
		"/v1/integrations/linear/publications",
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
	pubID = shell["publication_id"].(string)
	webhookURL := shell["webhook_url"].(string)
	expected := "http://test/linear/webhook/pub/" + pubID
	if webhookURL != expected {
		t.Fatalf("webhook_url=%q want %q", webhookURL, expected)
	}

	const webhookSecret = "lin_wh_smoke"
	credBody, _ := json.Marshal(map[string]any{
		"clientId":      "test-client",
		"clientSecret":  "test-secret",
		"webhookSecret": webhookSecret,
		"returnUrl":     "http://localhost/console",
	})
	req = httptest.NewRequest(
		http.MethodPatch,
		"/v1/integrations/linear/publications/"+pubID+"/credentials",
		bytes.NewReader(credBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("credentials status=%d body=%s", rec.Code, rec.Body.String())
	}

	bindBody := []byte(`{"workspace_id":"org_mock","bot_user_id":"bot_mock"}`)
	req = httptest.NewRequest(
		http.MethodPost,
		"/v1/internal/linear/publications/"+pubID+"/bind-mock-install",
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

func TestLinearWebhookCreatesSession(t *testing.T) {
	t.Setenv("INTEGRATIONS_GATEWAY_ORIGIN", "http://test")
	handler := testRouter(t)
	pubID := seedLinearLivePublication(t, handler)

	payload := []byte(`{
		"type": "AppUserNotification",
		"action": "issueAssignedToYou",
		"webhookId": "del_gateway_test",
		"organizationId": "org_mock",
		"notification": {
			"type": "issueAssignedToYou",
			"issue": {
				"id": "iss_142",
				"identifier": "ENG-142",
				"title": "Auth bug"
			},
			"actor": { "id": "usr_alice", "name": "Alice" }
		}
	}`)
	const webhookSecret = "lin_wh_smoke"
	sig := linearWebhookSignature(webhookSecret, payload)

	req := httptest.NewRequest(
		http.MethodPost,
		"/linear/webhook/pub/"+pubID,
		bytes.NewReader(payload),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("linear-signature", sig)
	req.Header.Set("linear-delivery", "del_gateway_test")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("webhook status=%d body=%s", rec.Code, rec.Body.String())
	}
	assertWebhookSessionCreated(t, handler, rec.Body.Bytes())
}
