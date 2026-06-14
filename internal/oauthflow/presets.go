package oauthflow

import (
	"fmt"
	"os"
	"strings"
)

// PresetClient holds operator-pre-registered OAuth app credentials.
type PresetClient struct {
	ClientID     string
	ClientSecret string
}

// ResolvePresetClient returns env-configured client credentials for known issuers.
func ResolvePresetClient(issuer string, callbackURI string) (*PresetClient, error) {
	issuer = strings.TrimRight(issuer, "/") + "/"
	switch {
	case strings.HasPrefix(issuer, "https://github.com/login/oauth/"):
		return presetFromEnv(
			"GITHUB_OAUTH_CLIENT_ID",
			"GITHUB_OAUTH_CLIENT_SECRET",
			"GitHub OAuth requires a pre-registered OAuth App: visit https://github.com/settings/developers, create an App with callback "+
				callbackURI+", then set GITHUB_OAUTH_CLIENT_ID + GITHUB_OAUTH_CLIENT_SECRET.",
		)
	case strings.HasPrefix(issuer, "https://accounts.feishu.cn/"):
		return presetFromEnv(
			"FEISHU_OAUTH_CLIENT_ID",
			"FEISHU_OAUTH_CLIENT_SECRET",
			"Feishu MCP OAuth requires a pre-registered Feishu app: visit https://open.feishu.cn, create a Web App with redirect URL "+
				callbackURI+", then set FEISHU_OAUTH_CLIENT_ID + FEISHU_OAUTH_CLIENT_SECRET.",
		)
	case strings.HasPrefix(issuer, "https://accounts.larksuite.com/"):
		return presetFromEnv(
			"LARK_OAUTH_CLIENT_ID",
			"LARK_OAUTH_CLIENT_SECRET",
			"Lark MCP OAuth requires a pre-registered Lark app: visit https://open.larksuite.com, create a Web App with redirect URL "+
				callbackURI+", then set LARK_OAUTH_CLIENT_ID + LARK_OAUTH_CLIENT_SECRET.",
		)
	case issuer == "https://app.asana.com/" ||
		strings.HasPrefix(issuer, "https://app.asana.com/"):
		return presetFromEnv(
			"ASANA_OAUTH_CLIENT_ID",
			"ASANA_OAUTH_CLIENT_SECRET",
			"Asana MCP OAuth requires a pre-registered Asana app: visit https://app.asana.com/0/my-apps, create an OAuth app with redirect URL "+
				callbackURI+", then set ASANA_OAUTH_CLIENT_ID + ASANA_OAUTH_CLIENT_SECRET.",
		)
	case issuer == "https://mcp.clickup.com/" ||
		strings.HasPrefix(issuer, "https://mcp.clickup.com/"):
		return presetFromEnv(
			"CLICKUP_OAUTH_CLIENT_ID",
			"CLICKUP_OAUTH_CLIENT_SECRET",
			"ClickUp MCP OAuth requires a pre-registered ClickUp app: visit https://app.clickup.com/settings/apps, create an OAuth app with redirect URL "+
				callbackURI+", then set CLICKUP_OAUTH_CLIENT_ID + CLICKUP_OAUTH_CLIENT_SECRET.",
		)
	case issuer == "https://slack.com/" ||
		strings.HasPrefix(issuer, "https://slack.com/"):
		return presetFromEnv(
			"SLACK_OAUTH_CLIENT_ID",
			"SLACK_OAUTH_CLIENT_SECRET",
			"Slack MCP OAuth requires a pre-registered Slack app: visit https://api.slack.com/apps, create an app with redirect URL "+
				callbackURI+", then set SLACK_OAUTH_CLIENT_ID + SLACK_OAUTH_CLIENT_SECRET.",
		)
	default:
		return nil, nil
	}
}

func presetFromEnv(idKey, secretKey, help string) (*PresetClient, error) {
	id := strings.TrimSpace(os.Getenv(idKey))
	secret := strings.TrimSpace(os.Getenv(secretKey))
	if id == "" || secret == "" {
		return nil, fmt.Errorf("%s", help)
	}
	return &PresetClient{ClientID: id, ClientSecret: secret}, nil
}
