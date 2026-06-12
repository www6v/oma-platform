package linear

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	authorizeURL = "https://linear.app/oauth/authorize"
	tokenURL     = "https://api.linear.app/oauth/token"
	graphqlURL   = "https://api.linear.app/graphql"
)

// DefaultScopes are requested at Linear OAuth install time.
var DefaultScopes = []string{
	"read",
	"write",
	"app:assignable",
	"app:mentionable",
}

// HTTPDoer is the subset of http.Client used for Linear API calls.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// TokenResponse is Linear's OAuth token payload.
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	RefreshToken string `json:"refresh_token"`
}

// ViewerInfo is the Linear bot user after OAuth.
type ViewerInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// OrganizationInfo is the Linear workspace after OAuth.
type OrganizationInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	URLKey string `json:"urlKey"`
}

// BuildAuthorizeURL redirects the admin to Linear's consent screen.
func BuildAuthorizeURL(
	clientID, redirectURI, state string,
	scopes []string,
) string {
	if len(scopes) == 0 {
		scopes = DefaultScopes
	}
	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("response_type", "code")
	params.Set("scope", strings.Join(scopes, ","))
	params.Set("state", state)
	params.Set("actor", "app")
	return authorizeURL + "?" + params.Encode()
}

// ExchangeAuthorizationCode trades an OAuth code for tokens.
func ExchangeAuthorizationCode(
	client HTTPDoer,
	code, redirectURI, clientID, clientSecret string,
) (TokenResponse, error) {
	var empty TokenResponse
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	form := url.Values{}
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	form.Set("grant_type", "authorization_code")
	req, err := http.NewRequest(
		http.MethodPost, tokenURL, strings.NewReader(form.Encode()),
	)
	if err != nil {
		return empty, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := client.Do(req)
	if err != nil {
		return empty, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return empty, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return empty, fmt.Errorf(
			"linear token exchange: %d %s",
			resp.StatusCode, truncate(string(body), 200),
		)
	}
	var token TokenResponse
	if err := json.Unmarshal(body, &token); err != nil {
		return empty, err
	}
	if token.AccessToken == "" {
		return empty, fmt.Errorf("linear token response missing access_token")
	}
	return token, nil
}

// FetchViewerAndOrg loads bot user + workspace with one GraphQL round trip.
func FetchViewerAndOrg(
	client HTTPDoer,
	accessToken string,
) (ViewerInfo, OrganizationInfo, error) {
	var viewer ViewerInfo
	var org OrganizationInfo
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	payload := map[string]any{
		"query": `query ViewerAndOrg {
			viewer { id name }
			organization { id name urlKey }
		}`,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return viewer, org, err
	}
	req, err := http.NewRequest(http.MethodPost, graphqlURL, strings.NewReader(string(raw)))
	if err != nil {
		return viewer, org, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+accessToken)
	resp, err := client.Do(req)
	if err != nil {
		return viewer, org, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return viewer, org, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return viewer, org, fmt.Errorf(
			"linear graphql: %d %s",
			resp.StatusCode, truncate(string(body), 200),
		)
	}
	var parsed struct {
		Data struct {
			Viewer       ViewerInfo       `json:"viewer"`
			Organization OrganizationInfo `json:"organization"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return viewer, org, err
	}
	if len(parsed.Errors) > 0 {
		return viewer, org, fmt.Errorf(
			"linear graphql: %s", parsed.Errors[0].Message,
		)
	}
	return parsed.Data.Viewer, parsed.Data.Organization, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
