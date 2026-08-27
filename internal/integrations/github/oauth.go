package github

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const githubAPI = "https://api.github.com"

// AppInfo is returned by GET /app.
type AppInfo struct {
	ID       string
	Slug     string
	BotLogin string
}

// PublicationCallbackURI builds the OAuth setup URL for a publication.
func PublicationCallbackURI(origin, pubID string) string {
	return strings.TrimRight(origin, "/") +
		"/github/oauth/pub/" + pubID + "/callback"
}

// BuildInstallURL returns GitHub's org-install URL for an App slug.
func BuildInstallURL(appSlug, state string) string {
	params := url.Values{}
	params.Set("state", state)
	return fmt.Sprintf(
		"https://github.com/apps/%s/installations/new?%s",
		url.PathEscape(appSlug), params.Encode(),
	)
}

// MintAppJWT signs a short-lived RS256 JWT for GitHub App API calls.
func MintAppJWT(privateKeyPEM, appID string) (string, error) {
	key, err := parseRSAPrivateKey(privateKeyPEM)
	if err != nil {
		return "", err
	}
	now := time.Now().Unix()
	header := map[string]string{"alg": "RS256", "typ": "JWT"}
	payload := map[string]any{
		"iat": now - 30,
		"exp": now + 540,
		"iss": appID,
	}
	headerB64, err := encodeSegment(header)
	if err != nil {
		return "", err
	}
	payloadB64, err := encodeSegment(payload)
	if err != nil {
		return "", err
	}
	signingInput := headerB64 + "." + payloadB64
	hash := sha256.Sum256([]byte(signingInput))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hash[:])
	if err != nil {
		return "", fmt.Errorf("sign app jwt: %w", err)
	}
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

// GetApp fetches GitHub App metadata using an App JWT.
func GetApp(client *http.Client, appJWT string) (AppInfo, error) {
	var empty AppInfo
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	req, err := http.NewRequest(http.MethodGet, githubAPI+"/app", nil)
	if err != nil {
		return empty, err
	}
	req.Header.Set("Authorization", "Bearer "+appJWT)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "meta-harness")
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
		return empty, fmt.Errorf("github GET /app: %s", string(body))
	}
	var parsed struct {
		ID      int64  `json:"id"`
		Slug    string `json:"slug"`
		Owner   struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name    string `json:"name"`
		HTMLURL string `json:"html_url"`
		Bot     *struct {
			Login string `json:"login"`
		} `json:"bot"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return empty, err
	}
	botLogin := ""
	if parsed.Bot != nil {
		botLogin = parsed.Bot.Login
	}
	if botLogin == "" {
		botLogin = parsed.Slug + "[bot]"
	}
	return AppInfo{
		ID:       fmt.Sprintf("%d", parsed.ID),
		Slug:     parsed.Slug,
		BotLogin: botLogin,
	}, nil
}

func parseRSAPrivateKey(pemText string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return nil, fmt.Errorf("invalid private key PEM")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		key, err = x509.ParsePKCS1PrivateKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("parse private key: %w", err)
		}
	}
	rsaKey, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not RSA")
	}
	return rsaKey, nil
}

func encodeSegment(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
