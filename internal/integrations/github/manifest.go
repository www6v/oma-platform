package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const manifestHomepageURL = "https://openma.dev"

// ManifestInput configures a GitHub App manifest POST body.
type ManifestInput struct {
	Name        string
	WebhookURL  string
	RedirectURL string
	SetupURL    string
	Permissions map[string]string
	Events      []string
	Public      bool
}

// ManifestConversion holds credentials returned by GitHub manifest exchange.
type ManifestConversion struct {
	ID            int64
	Slug          string
	BotLogin      string
	ClientID      string
	ClientSecret  string
	WebhookSecret string
	PEM           string
}

// BuildManifest returns the JSON GitHub expects in the manifest form POST.
func BuildManifest(in ManifestInput) map[string]any {
	perms := in.Permissions
	if perms == nil {
		perms = map[string]string{
			"contents": "write", "issues": "write",
			"pull_requests": "write", "metadata": "read", "actions": "read",
		}
	}
	events := in.Events
	if len(events) == 0 {
		events = []string{
			"issues", "issue_comment", "pull_request",
			"pull_request_review", "pull_request_review_comment",
		}
	}
	return map[string]any{
		"name": in.Name,
		"url":  manifestHomepageURL,
		"hook_attributes": map[string]any{
			"url":    in.WebhookURL,
			"active": true,
		},
		"redirect_url":  in.RedirectURL,
		"callback_urls": []string{in.SetupURL},
		"setup_url":     in.SetupURL,
		"setup_on_update": true,
		"public":          in.Public,
		"default_permissions": perms,
		"default_events":      events,
	}
}

// ManifestCallbackURI is where GitHub redirects after manifest App creation.
func ManifestCallbackURI(origin string) string {
	return strings.TrimRight(origin, "/") + "/github/manifest/callback"
}

// ExchangeManifestCode converts a one-time manifest code into App credentials.
func ExchangeManifestCode(
	client *http.Client,
	code string,
) (ManifestConversion, error) {
	var empty ManifestConversion
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	url := fmt.Sprintf(
		"%s/app-manifests/%s/conversions",
		githubAPI, code,
	)
	req, err := http.NewRequest(http.MethodPost, url, nil)
	if err != nil {
		return empty, err
	}
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
			"manifest conversion: HTTP %d %s",
			resp.StatusCode, string(body),
		)
	}
	return parseManifestConversion(body)
}

func parseManifestConversion(body []byte) (ManifestConversion, error) {
	var empty ManifestConversion
	var parsed struct {
		ID            int64  `json:"id"`
		Slug          string `json:"slug"`
		ClientID      string `json:"client_id"`
		ClientSecret  string `json:"client_secret"`
		WebhookSecret string `json:"webhook_secret"`
		PEM           string `json:"pem"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return empty, err
	}
	if parsed.ID == 0 || parsed.Slug == "" ||
		parsed.PEM == "" || parsed.WebhookSecret == "" {
		return empty, fmt.Errorf(
			"manifest conversion: missing required fields",
		)
	}
	return ManifestConversion{
		ID:            parsed.ID,
		Slug:          parsed.Slug,
		BotLogin:      parsed.Slug + "[bot]",
		ClientID:      parsed.ClientID,
		ClientSecret:  parsed.ClientSecret,
		WebhookSecret: parsed.WebhookSecret,
		PEM:           parsed.PEM,
	}, nil
}
