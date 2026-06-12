package oauthstate

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const defaultTTL = 15 * time.Minute

// LinearPublicationPayload is signed into OAuth state for publication install.
type LinearPublicationPayload struct {
	Kind          string `json:"kind"`
	PublicationID string `json:"publicationId"`
	ReturnURL     string `json:"returnUrl"`
	Nonce         string `json:"nonce"`
	Exp           int64  `json:"exp"`
}

// SignLinearPublication builds an HMAC-signed state token.
func SignLinearPublication(
	secret string,
	payload LinearPublicationPayload,
) (string, error) {
	if secret == "" {
		return "", errors.New("oauth state secret required")
	}
	if payload.Exp == 0 {
		payload.Exp = time.Now().Add(defaultTTL).Unix()
	}
	if payload.Kind == "" {
		payload.Kind = "linear.oauth.publication"
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	body := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(body))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return body + "." + sig, nil
}

// VerifyLinearPublication validates and decodes a signed state token.
func VerifyLinearPublication(
	secret, token string,
) (LinearPublicationPayload, error) {
	var empty LinearPublicationPayload
	if secret == "" {
		return empty, errors.New("oauth state secret required")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return empty, errors.New("invalid state token")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return empty, errors.New("invalid state signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return empty, fmt.Errorf("decode state: %w", err)
	}
	var payload LinearPublicationPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return empty, err
	}
	if payload.Kind != "linear.oauth.publication" {
		return empty, errors.New("invalid state kind")
	}
	if payload.Exp > 0 && time.Now().Unix() > payload.Exp {
		return empty, errors.New("state expired")
	}
	return payload, nil
}
