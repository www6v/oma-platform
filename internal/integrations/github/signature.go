package github

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// VerifyWebhookSignature checks GitHub X-Hub-Signature-256.
func VerifyWebhookSignature(secret string, rawBody []byte, signature string) bool {
	if secret == "" || signature == "" {
		return false
	}
	const prefix = "sha256="
	if !strings.HasPrefix(signature, prefix) {
		return false
	}
	expectedHex := strings.TrimPrefix(signature, prefix)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(rawBody)
	actualHex := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(actualHex), []byte(expectedHex))
}
