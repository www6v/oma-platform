package linear_test

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/open-ma/oma-building/internal/integrations/linear"
)

func signWebhook(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestVerifyWebhookSignature(t *testing.T) {
	secret := "lin_wh_test"
	body := []byte(`{"webhookId":"del_1"}`)
	sig := signWebhook(secret, body)
	if !linear.VerifyWebhookSignature(secret, string(body), sig) {
		t.Fatal("expected valid signature")
	}
	if linear.VerifyWebhookSignature(secret, string(body), "bad") {
		t.Fatal("expected invalid signature")
	}
}

func TestParseWebhookIssueAssigned(t *testing.T) {
	raw := []byte(`{
		"type": "AppUserNotification",
		"action": "issueAssignedToYou",
		"webhookId": "del_xyz",
		"organizationId": "org_acme",
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
	event, err := linear.ParseWebhook(raw)
	if err != nil {
		t.Fatal(err)
	}
	if event == nil {
		t.Fatal("expected event")
	}
	if event.Kind != linear.KindIssueAssignedToYou {
		t.Fatalf("kind=%q", event.Kind)
	}
	if event.IssueIdentifier != "ENG-142" {
		t.Fatalf("identifier=%q", event.IssueIdentifier)
	}
}

func TestBuildUserMessageEvent(t *testing.T) {
	event, err := linear.ParseWebhook([]byte(`{
		"type": "AppUserNotification",
		"action": "issueMention",
		"webhookId": "del_2",
		"organizationId": "org_acme",
		"notification": {
			"type": "issueMention",
			"issue": { "id": "iss_1", "identifier": "ENG-1", "title": "T" },
			"actor": { "id": "u1", "name": "Bob" }
		}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := linear.BuildUserMessageEvent(event, "pub_test")
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["type"] != "user.message" {
		t.Fatalf("type=%v", payload["type"])
	}
}
