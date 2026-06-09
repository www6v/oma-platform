package store

import (
	"encoding/base64"
	"encoding/json"
	"errors"
)

const (
	// DefaultListLimit is the page size when limit is omitted.
	DefaultListLimit = 20
	// MaxListLimit caps list endpoints.
	MaxListLimit = 200
)

// PageCursor is the keyset pagination token payload.
type PageCursor struct {
	CreatedAt int64  `json:"created_at"`
	ID        string `json:"id"`
}

// EncodePageCursor returns a URL-safe opaque cursor string.
func EncodePageCursor(c PageCursor) (string, error) {
	raw, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// DecodePageCursor parses an opaque cursor. Empty input is not an error.
func DecodePageCursor(raw string) (*PageCursor, error) {
	if raw == "" {
		return nil, nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return nil, errors.New("invalid cursor")
	}
	var c PageCursor
	if err := json.Unmarshal(decoded, &c); err != nil {
		return nil, errors.New("invalid cursor")
	}
	if c.ID == "" {
		return nil, errors.New("invalid cursor")
	}
	return &c, nil
}

// ClampLimit normalizes list limit query values.
func ClampLimit(limit int) int {
	if limit <= 0 {
		return DefaultListLimit
	}
	if limit > MaxListLimit {
		return MaxListLimit
	}
	return limit
}
