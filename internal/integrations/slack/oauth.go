package slack

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

const slackAuthorizeURL = "https://slack.com/oauth/v2/authorize"
const slackNewAppURL = "https://api.slack.com/apps"

var defaultBotScopes = []string{
	"app_mentions:read",
	"assistant:write",
	"chat:write",
	"chat:write.public",
	"channels:history",
	"groups:history",
	"im:history",
	"mpim:history",
	"channels:read",
	"groups:read",
	"reactions:read",
	"reactions:write",
	"users:read",
	"users:read.email",
	"team:read",
}

var defaultUserScopes = []string{
	"search:read.public",
	"search:read.private",
	"search:read.im",
	"search:read.mpim",
	"channels:history",
	"groups:history",
	"im:history",
	"mpim:history",
	"users:read",
	"canvases:read",
	"canvases:write",
}

var defaultSubscribedEvents = []string{
	"app_mention",
	"message.channels",
	"message.im",
	"message.groups",
	"message.mpim",
	"member_joined_channel",
	"member_left_channel",
	"reaction_added",
	"channel_archive",
	"channel_unarchive",
	"channel_rename",
}

// PublicationCallbackURI builds the OAuth redirect URI for a publication.
func PublicationCallbackURI(origin, pubID string) string {
	return strings.TrimRight(origin, "/") +
		"/slack/oauth/pub/" + pubID + "/callback"
}

// BuildAuthorizeURL returns Slack OAuth v2 authorize URL.
func BuildAuthorizeURL(
	clientID, redirectURI, state string,
	botScopes, userScopes []string,
) string {
	if len(botScopes) == 0 {
		botScopes = defaultBotScopes
	}
	if len(userScopes) == 0 {
		userScopes = defaultUserScopes
	}
	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", strings.Join(botScopes, ","))
	params.Set("user_scope", strings.Join(userScopes, ","))
	params.Set("state", state)
	return slackAuthorizeURL + "?" + params.Encode()
}

// BuildManifestLaunchURL returns Slack's "Create from manifest" URL.
func BuildManifestLaunchURL(
	personaName, webhookURL, redirectURL string,
) string {
	manifest := buildManifest(personaName, webhookURL, redirectURL)
	raw, _ := json.Marshal(manifest)
	params := url.Values{}
	params.Set("new_app", "1")
	params.Set("manifest_json", string(raw))
	return slackNewAppURL + "?" + params.Encode()
}

func buildManifest(
	personaName, webhookURL, redirectURL string,
) map[string]any {
	botEvents := append([]string{}, defaultSubscribedEvents...)
	botEvents = append(botEvents,
		"assistant_thread_started",
		"assistant_thread_context_changed",
	)
	seen := map[string]struct{}{}
	uniqueEvents := make([]string, 0, len(botEvents))
	for _, ev := range botEvents {
		if _, ok := seen[ev]; ok {
			continue
		}
		seen[ev] = struct{}{}
		uniqueEvents = append(uniqueEvents, ev)
	}
	botScopes := append([]string{}, defaultBotScopes...)
	hasAssistant := false
	for _, s := range botScopes {
		if s == "assistant:write" {
			hasAssistant = true
			break
		}
	}
	if !hasAssistant {
		botScopes = append(botScopes, "assistant:write")
	}
	return map[string]any{
		"display_information": map[string]any{
			"name":        personaName,
			"description": fmt.Sprintf("%s — an OpenMA agent", personaName),
		},
		"features": map[string]any{
			"app_home": map[string]any{
				"home_tab_enabled":              false,
				"messages_tab_enabled":          true,
				"messages_tab_read_only_enabled": false,
			},
			"bot_user": map[string]any{
				"display_name":  personaName,
				"always_online": true,
			},
			"assistant_view": map[string]any{
				"assistant_description": fmt.Sprintf(
					"Chat with %s in a side pane.", personaName,
				),
				"suggested_prompts": []any{},
			},
		},
		"oauth_config": map[string]any{
			"redirect_urls": []string{redirectURL},
			"scopes": map[string]any{
				"bot":  botScopes,
				"user": defaultUserScopes,
			},
		},
		"settings": map[string]any{
			"event_subscriptions": map[string]any{
				"request_url": webhookURL,
				"bot_events":  uniqueEvents,
			},
			"interactivity": map[string]any{
				"is_enabled":  true,
				"request_url": webhookURL,
			},
			"is_mcp_enabled":          true,
			"org_deploy_enabled":      false,
			"socket_mode_enabled":     false,
			"token_rotation_enabled":  false,
		},
	}
}
