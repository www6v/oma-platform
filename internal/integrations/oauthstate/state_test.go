package oauthstate_test

import (
	"strings"
	"testing"
	"time"

	"github.com/open-ma/oma-building/internal/integrations/oauthstate"
)

func TestSignAndVerifyLinearPublication(t *testing.T) {
	token, err := oauthstate.SignLinearPublication("secret", oauthstate.LinearPublicationPayload{
		PublicationID: "pub_1",
		ReturnURL:     "http://localhost/callback",
		Nonce:         "n1",
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := oauthstate.VerifyLinearPublication("secret", token)
	if err != nil {
		t.Fatal(err)
	}
	if payload.PublicationID != "pub_1" {
		t.Fatalf("publication=%q", payload.PublicationID)
	}
	if payload.ReturnURL != "http://localhost/callback" {
		t.Fatalf("return_url=%q", payload.ReturnURL)
	}
}

func TestVerifyLinearPublicationRejectsTamper(t *testing.T) {
	token, err := oauthstate.SignLinearPublication("secret", oauthstate.LinearPublicationPayload{
		PublicationID: "pub_1",
		Nonce:         "n1",
	})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	parts[0] = parts[0] + "x"
	if _, err := oauthstate.VerifyLinearPublication("secret", parts[0]+"."+parts[1]); err == nil {
		t.Fatal("expected tamper rejection")
	}
}

func TestVerifyLinearPublicationRejectsExpired(t *testing.T) {
	token, err := oauthstate.SignLinearPublication("secret", oauthstate.LinearPublicationPayload{
		PublicationID: "pub_1",
		Nonce:         "n1",
		Exp:           time.Now().Add(-time.Hour).Unix(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oauthstate.VerifyLinearPublication("secret", token); err == nil {
		t.Fatal("expected expiry rejection")
	}
}
