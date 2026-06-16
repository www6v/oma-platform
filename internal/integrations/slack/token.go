package slack

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const slackTokenURL = "https://slack.com/api/oauth.v2.access"

// TokenExchange holds Slack OAuth v2 token response fields.
type TokenExchange struct {
	BotToken   string
	BotUserID  string
	AppID      string
	TeamID     string
	TeamName   string
	UserToken  string
	UserScopes string
	BotScopes  string
}

// ExchangeOAuthCode trades an authorization code for bot + user tokens.
func ExchangeOAuthCode(
	client *http.Client,
	code, redirectURI, clientID, clientSecret string,
) (TokenExchange, error) {
	var empty TokenExchange
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	form := url.Values{}
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	req, err := http.NewRequest(
		http.MethodPost, slackTokenURL, strings.NewReader(form.Encode()),
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
	if resp.StatusCode >= 400 {
		return empty, fmt.Errorf(
			"slack oauth.v2.access: HTTP %d %s",
			resp.StatusCode, string(body),
		)
	}
	return parseTokenExchange(body)
}

func parseTokenExchange(body []byte) (TokenExchange, error) {
	var empty TokenExchange
	var parsed struct {
		OK          bool   `json:"ok"`
		Error       string `json:"error"`
		AccessToken string `json:"access_token"`
		Scope       string `json:"scope"`
		BotUserID   string `json:"bot_user_id"`
		AppID       string `json:"app_id"`
		Team        struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"team"`
		AuthedUser struct {
			AccessToken string `json:"access_token"`
			Scope       string `json:"scope"`
		} `json:"authed_user"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return empty, err
	}
	if !parsed.OK {
		msg := parsed.Error
		if msg == "" {
			msg = "unknown_error"
		}
		return empty, fmt.Errorf("slack oauth: %s", msg)
	}
	if !strings.HasPrefix(parsed.AccessToken, "xoxb-") {
		return empty, fmt.Errorf("slack oauth: missing bot access_token")
	}
	if parsed.BotUserID == "" || parsed.Team.ID == "" || parsed.Team.Name == "" {
		return empty, fmt.Errorf("slack oauth: missing team or bot_user_id")
	}
	if !strings.HasPrefix(parsed.AuthedUser.AccessToken, "xoxp-") {
		return empty, fmt.Errorf(
			"slack oauth: missing user access_token (xoxp-)",
		)
	}
	return TokenExchange{
		BotToken:   parsed.AccessToken,
		BotUserID:  parsed.BotUserID,
		AppID:      parsed.AppID,
		TeamID:     parsed.Team.ID,
		TeamName:   parsed.Team.Name,
		UserToken:  parsed.AuthedUser.AccessToken,
		UserScopes: parsed.AuthedUser.Scope,
		BotScopes:  parsed.Scope,
	}, nil
}
