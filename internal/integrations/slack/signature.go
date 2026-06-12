package slack

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const maxClockSkew = 5 * time.Minute

// VerifyWebhookSignature checks Slack v0 signing secret.
func VerifyWebhookSignature(
	secret string,
	rawBody []byte,
	timestamp, signature string,
) bool {
	if secret == "" || timestamp == "" || signature == "" {
		return false
	}
	if !strings.HasPrefix(signature, "v0=") {
		return false
	}
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return false
	}
	now := time.Now().Unix()
	if ts < now-int64(maxClockSkew.Seconds()) ||
		ts > now+int64(maxClockSkew.Seconds()) {
		return false
	}
	base := fmt.Sprintf("v0:%s:%s", timestamp, string(rawBody))
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(base))
	expected := "v0=" + hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(signature))
}
