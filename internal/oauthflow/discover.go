package oauthflow

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

// ProtectedResourceMeta is RFC 9728 protected resource metadata.
type ProtectedResourceMeta struct {
	Resource              string   `json:"resource"`
	AuthorizationServers  []string `json:"authorization_servers"`
	ScopesSupported       []string `json:"scopes_supported"`
}

// AuthServerMeta is OAuth authorization server metadata.
type AuthServerMeta struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	RegistrationEndpoint  string `json:"registration_endpoint"`
	ScopesSupported       []string `json:"scopes_supported"`
}

// DiscoveredOAuth pairs PRM + ASM for an MCP server URL.
type DiscoveredOAuth struct {
	Resource   ProtectedResourceMeta
	AuthServer AuthServerMeta
}

// DiscoverOAuthMeta follows MCP OAuth metadata discovery for an MCP URL.
func DiscoverOAuthMeta(
	ctx context.Context,
	client *http.Client,
	mcpServerURL string,
) (DiscoveredOAuth, error) {
	if client == nil {
		client = http.DefaultClient
	}
	u, err := url.Parse(mcpServerURL)
	if err != nil {
		return DiscoveredOAuth{}, err
	}
	origin := u.Scheme + "//" + u.Host
	path := strings.TrimRight(u.Path, "/")

	candidates := []string{}
	if path != "" {
		candidates = append(
			candidates,
			origin+"/.well-known/oauth-protected-resource"+path,
		)
	}
	candidates = append(
		candidates,
		origin+"/.well-known/oauth-protected-resource",
	)

	var resource ProtectedResourceMeta
	var lastErr string
	for _, candidate := range candidates {
		res, err := fetchJSON(ctx, client, candidate)
		if err != nil {
			lastErr = candidate + ": " + err.Error()
			continue
		}
		if err := json.Unmarshal(res, &resource); err != nil {
			lastErr = candidate + ": invalid json"
			continue
		}
		lastErr = ""
		break
	}
	if lastErr != "" {
		return DiscoveredOAuth{}, fmt.Errorf(
			"Failed to fetch Protected Resource Metadata (tried %d): %s",
			len(candidates), lastErr,
		)
	}
	if len(resource.AuthorizationServers) == 0 {
		return DiscoveredOAuth{}, fmt.Errorf(
			"No authorization_servers in Protected Resource Metadata",
		)
	}

	authServerURL := resource.AuthorizationServers[0]
	asmIssuer, err := url.Parse(authServerURL)
	if err != nil {
		return DiscoveredOAuth{}, err
	}
	asmOrigin := asmIssuer.Scheme + "//" + asmIssuer.Host
	asmPath := strings.TrimRight(asmIssuer.Path, "/")

	asmCandidates := []string{}
	if asmPath != "" {
		asmCandidates = append(
			asmCandidates,
			asmOrigin+"/.well-known/oauth-authorization-server"+asmPath,
			asmOrigin+"/.well-known/openid-configuration"+asmPath,
		)
	}
	asmCandidates = append(
		asmCandidates,
		asmOrigin+"/.well-known/oauth-authorization-server",
		asmOrigin+"/.well-known/openid-configuration",
		asmIssuer.String()+"/.well-known/oauth-authorization-server",
	)

	var authServer AuthServerMeta
	asmLastErr := ""
	for _, candidate := range asmCandidates {
		res, err := fetchJSON(ctx, client, candidate)
		if err != nil {
			asmLastErr = candidate + ": " + err.Error()
			continue
		}
		if err := json.Unmarshal(res, &authServer); err != nil {
			asmLastErr = candidate + ": invalid json"
			continue
		}
		asmLastErr = ""
		break
	}

	githubIssuer := regexp.MustCompile(`^https://github\.com/login/oauth/?$`)
	if authServer.AuthorizationEndpoint == "" &&
		githubIssuer.MatchString(authServerURL) {
		authServer = AuthServerMeta{
			Issuer:                "https://github.com/login/oauth",
			AuthorizationEndpoint: "https://github.com/login/oauth/authorize",
			TokenEndpoint:         "https://github.com/login/oauth/access_token",
		}
	}

	if authServer.AuthorizationEndpoint == "" || authServer.TokenEndpoint == "" {
		if asmLastErr != "" {
			return DiscoveredOAuth{}, fmt.Errorf(
				"Failed to fetch Auth Server Metadata (tried %d): %s",
				len(asmCandidates), asmLastErr,
			)
		}
		return DiscoveredOAuth{}, fmt.Errorf(
			"Auth Server Metadata missing authorization_endpoint or token_endpoint",
		)
	}

	return DiscoveredOAuth{Resource: resource, AuthServer: authServer}, nil
}

func fetchJSON(
	ctx context.Context,
	client *http.Client,
	url string,
) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// DynamicClientRegistration attempts RFC 7591 registration when supported.
func DynamicClientRegistration(
	ctx context.Context,
	client *http.Client,
	registrationEndpoint string,
	redirectURI string,
) (clientID, clientSecret string, ok bool) {
	if client == nil {
		client = http.DefaultClient
	}
	payload := map[string]any{
		"client_name":                "Open Managed Agents",
		"redirect_uris":              []string{redirectURI},
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"token_endpoint_auth_method": "none",
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", "", false
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, registrationEndpoint, strings.NewReader(string(body)),
	)
	if err != nil {
		return "", "", false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil || resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp != nil {
			resp.Body.Close()
		}
		return "", "", false
	}
	defer resp.Body.Close()
	var data struct {
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", "", false
	}
	if data.ClientID == "" {
		return "", "", false
	}
	return data.ClientID, data.ClientSecret, true
}
