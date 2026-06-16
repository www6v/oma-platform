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

const formTokenTTL = 60 * time.Minute

// FormTokenPayload is signed into install wizard JWTs.
type FormTokenPayload struct {
	Kind          string `json:"kind"`
	PublicationID string `json:"publicationId"`
	AppOmaID      string `json:"appOmaId,omitempty"`
	UserID        string `json:"userId"`
	ReturnURL     string `json:"returnUrl"`
	Handoff       bool   `json:"handoff,omitempty"`
	Exp           int64  `json:"exp"`
}

// SignFormToken builds an HMAC-signed form token.
func SignFormToken(
	secret string,
	payload FormTokenPayload,
	ttl time.Duration,
) (string, error) {
	if secret == "" {
		return "", errors.New("form token secret required")
	}
	if payload.Kind == "" {
		return "", errors.New("form token kind required")
	}
	if ttl <= 0 {
		ttl = formTokenTTL
	}
	if payload.Exp == 0 {
		payload.Exp = time.Now().Add(ttl).Unix()
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

// VerifyFormToken validates and decodes a signed form token.
func VerifyFormToken(
	secret, token, expectedKind string,
) (FormTokenPayload, error) {
	var empty FormTokenPayload
	if secret == "" {
		return empty, errors.New("form token secret required")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return empty, errors.New("invalid form token")
	}
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(parts[0]))
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(expected), []byte(parts[1])) {
		return empty, errors.New("invalid form token signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return empty, fmt.Errorf("decode form token: %w", err)
	}
	var payload FormTokenPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return empty, err
	}
	if expectedKind != "" && payload.Kind != expectedKind {
		return empty, errors.New("invalid form token kind")
	}
	if payload.Exp > 0 && time.Now().Unix() > payload.Exp {
		return empty, errors.New("form token expired")
	}
	return payload, nil
}
