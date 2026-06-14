package oauthflow

import (
	"testing"
)

func TestSha256Base64URLDeterministic(t *testing.T) {
	a := Sha256Base64URL("verifier")
	b := Sha256Base64URL("verifier")
	if a != b || a == "" {
		t.Fatalf("unexpected hash: %q", a)
	}
}

func TestRandomHexLength(t *testing.T) {
	h, err := RandomHex(32)
	if err != nil {
		t.Fatal(err)
	}
	if len(h) != 32 {
		t.Fatalf("len=%d", len(h))
	}
}
