package vaultoauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const defaultClientID = "open-managed-agents"

// RefreshMetadata holds OAuth refresh fields from an mcp_oauth credential.
type RefreshMetadata struct {
	RefreshToken  string
	TokenEndpoint string
	ClientID      string
	ClientSecret  string
}

// RefreshedTokens is the token response from a successful refresh.
type RefreshedTokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    *int
}

// RefreshMetadataOf extracts refresh metadata from stored auth JSON.
func RefreshMetadataOf(auth json.RawMessage) (*RefreshMetadata, error) {
	var meta struct {
		Type          string `json:"type"`
		RefreshToken  string `json:"refresh_token"`
		TokenEndpoint string `json:"token_endpoint"`
		ClientID      string `json:"client_id"`
		ClientSecret  string `json:"client_secret"`
	}
	if err := json.Unmarshal(auth, &meta); err != nil {
		return nil, err
	}
	if meta.Type != "mcp_oauth" {
		return nil, nil
	}
	if meta.RefreshToken == "" || meta.TokenEndpoint == "" {
		return nil, nil
	}
	return &RefreshMetadata{
		RefreshToken:  meta.RefreshToken,
		TokenEndpoint: meta.TokenEndpoint,
		ClientID:      meta.ClientID,
		ClientSecret:  meta.ClientSecret,
	}, nil
}

// RefreshMcpOAuth POSTs refresh_token to token_endpoint and returns new tokens.
func RefreshMcpOAuth(
	ctx context.Context,
	meta RefreshMetadata,
	client *http.Client,
) (*RefreshedTokens, error) {
	if client == nil {
		client = http.DefaultClient
	}
	clientID := meta.ClientID
	if clientID == "" {
		clientID = defaultClientID
	}

	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", meta.RefreshToken)
	form.Set("client_id", clientID)
	if meta.ClientSecret != "" {
		form.Set("client_secret", meta.ClientSecret)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		meta.TokenEndpoint,
		strings.NewReader(form.Encode()),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("token endpoint status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var tokens struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    *int   `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tokens); err != nil {
		return nil, err
	}
	if tokens.AccessToken == "" {
		return nil, fmt.Errorf("missing access_token in refresh response")
	}
	refreshToken := tokens.RefreshToken
	if refreshToken == "" {
		refreshToken = meta.RefreshToken
	}
	return &RefreshedTokens{
		AccessToken:  tokens.AccessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    tokens.ExpiresIn,
	}, nil
}

// AuthPatchForRefresh builds a partial auth JSON patch after refresh.
func AuthPatchForRefresh(tokens RefreshedTokens) (json.RawMessage, error) {
	patch := map[string]any{
		"access_token":  tokens.AccessToken,
		"refresh_token": tokens.RefreshToken,
	}
	if tokens.ExpiresIn != nil {
		expiresAt := time.Now().Add(
			time.Duration(*tokens.ExpiresIn) * time.Second,
		).UTC().Format(time.RFC3339Nano)
		patch["expires_at"] = expiresAt
	}
	return json.Marshal(patch)
}
