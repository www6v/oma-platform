package oauthstate_test

import (
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/integrations/oauthstate"
)

func TestFormTokenRoundTrip(t *testing.T) {
	t.Parallel()
	secret := "test-form-token-secret"
	token, err := oauthstate.SignFormToken(secret, oauthstate.FormTokenPayload{
		Kind:          "github.pub.form",
		PublicationID: "pub_test",
		AppOmaID:      "ghapp_test",
		UserID:        "default",
		ReturnURL:     "http://localhost/console",
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := oauthstate.VerifyFormToken(
		secret, token, "github.pub.form",
	)
	if err != nil {
		t.Fatal(err)
	}
	if payload.PublicationID != "pub_test" {
		t.Fatalf("publicationId=%q", payload.PublicationID)
	}
	if payload.AppOmaID != "ghapp_test" {
		t.Fatalf("appOmaId=%q", payload.AppOmaID)
	}
}

func TestFormTokenWrongKind(t *testing.T) {
	t.Parallel()
	secret := "test-form-token-secret"
	token, err := oauthstate.SignFormToken(secret, oauthstate.FormTokenPayload{
		Kind:          "slack.pub.form",
		PublicationID: "pub_test",
		UserID:        "default",
	}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	_, err = oauthstate.VerifyFormToken(secret, token, "github.pub.form")
	if err == nil {
		t.Fatal("expected kind mismatch error")
	}
}
