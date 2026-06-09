package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SessionResolver loads better-auth sessions from the auth upstream.
type SessionResolver struct {
	Upstream string
	Client   *http.Client
}

// Resolve returns the authenticated user for cookie headers, or nil.
func (r *SessionResolver) Resolve(
	ctx context.Context,
	headers http.Header,
) (*User, error) {
	if r == nil || strings.TrimSpace(r.Upstream) == "" {
		return nil, nil
	}
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	url := strings.TrimRight(r.Upstream, "/") + "/auth/get-session"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("session request: %w", err)
	}
	if cookie := headers.Get("Cookie"); cookie != "" {
		req.Header.Set("Cookie", cookie)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("session upstream: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("session read: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("session status %d: %s", resp.StatusCode, body)
	}

	var payload struct {
		User *struct {
			ID    string `json:"id"`
			Email string `json:"email"`
			Name  string `json:"name"`
		} `json:"user"`
		Session *struct {
			UserID string `json:"userId"`
		} `json:"session"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("session decode: %w", err)
	}
	if payload.User == nil || payload.User.ID == "" {
		return nil, nil
	}
	return &User{
		ID:    payload.User.ID,
		Email: payload.User.Email,
		Name:  payload.User.Name,
	}, nil
}
