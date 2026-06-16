package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// InstallationDetail is returned by GET /app/installations/:id.
type InstallationDetail struct {
	ID      int64
	Account struct {
		Login string
		Type  string
	}
}

// MintInstallationToken exchanges an App JWT for a 1-hour installation token.
func MintInstallationToken(
	client *http.Client,
	appJWT, installationID string,
) (string, error) {
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	url := fmt.Sprintf(
		"%s/app/installations/%s/access_tokens",
		githubAPI, installationID,
	)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "oma-platform")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 400 {
		return "", fmt.Errorf(
			"github installation token: HTTP %d %s",
			resp.StatusCode, string(body),
		)
	}
	var parsed struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if parsed.Token == "" {
		return "", fmt.Errorf("github installation token: missing token")
	}
	return parsed.Token, nil
}

// GetInstallation fetches install metadata using an App JWT.
func GetInstallation(
	client *http.Client,
	appJWT, installationID string,
) (InstallationDetail, error) {
	var empty InstallationDetail
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	url := fmt.Sprintf(
		"%s/app/installations/%s",
		githubAPI, installationID,
	)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return empty, err
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "oma-platform")
	resp, err := client.Do(req)
	if err != nil {
		return empty, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return empty, err
	}
	if resp.StatusCode >= 400 {
		return empty, fmt.Errorf(
			"github GET /app/installations/%s: HTTP %d %s",
			installationID, resp.StatusCode, string(body),
		)
	}
	var parsed struct {
		ID      int64 `json:"id"`
		Account struct {
			Login string `json:"login"`
			Type  string `json:"type"`
		} `json:"account"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return empty, err
	}
	if parsed.ID == 0 || parsed.Account.Login == "" {
		return empty, fmt.Errorf("github installation: malformed response")
	}
	out := InstallationDetail{ID: parsed.ID}
	out.Account.Login = parsed.Account.Login
	out.Account.Type = parsed.Account.Type
	return out, nil
}
