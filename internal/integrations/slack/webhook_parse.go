package slack

import (
	"encoding/json"
	"fmt"
)

const KindAppMention = "app_mention"

// NormalizedEvent is a Slack delivery narrowed to dispatch fields.
type NormalizedEvent struct {
	Kind        string
	DeliveryID  string
	WorkspaceID string
	ChannelID   string
	ThreadTS    string
	ScopeKey    string
	Text        string
	UserID      string
}

type rawEnvelope struct {
	Type      string          `json:"type"`
	Challenge string          `json:"challenge"`
	EventID   string          `json:"event_id"`
	TeamID    string          `json:"team_id"`
	Event     json.RawMessage `json:"event"`
}

type rawEvent struct {
	Type     string `json:"type"`
	User     string `json:"user"`
	Text     string `json:"text"`
	Channel  string `json:"channel"`
	TS       string `json:"ts"`
	ThreadTS string `json:"thread_ts"`
	BotID    string `json:"bot_id"`
	Subtype  string `json:"subtype"`
}

// ParseResult holds handshake or normalized event data.
type ParseResult struct {
	URLVerification bool
	Challenge       string
	Event           *NormalizedEvent
}

// ParseWebhook parses a Slack Events API delivery.
func ParseWebhook(rawBody []byte) (*ParseResult, error) {
	var env rawEnvelope
	if err := json.Unmarshal(rawBody, &env); err != nil {
		return nil, err
	}
	switch env.Type {
	case "url_verification":
		return &ParseResult{
			URLVerification: true,
			Challenge:       env.Challenge,
		}, nil
	case "event_callback":
		var inner rawEvent
		if err := json.Unmarshal(env.Event, &inner); err != nil {
			return nil, err
		}
		if inner.BotID != "" || inner.Subtype == "bot_message" {
			return &ParseResult{}, nil
		}
		event := &NormalizedEvent{
			DeliveryID:  env.EventID,
			WorkspaceID: env.TeamID,
			ChannelID:   inner.Channel,
			ThreadTS:    inner.ThreadTS,
			Text:        inner.Text,
			UserID:      inner.User,
		}
		if event.ThreadTS == "" {
			event.ThreadTS = inner.TS
		}
		if inner.Type == "app_mention" {
			event.Kind = KindAppMention
			event.ScopeKey = scopeKey(event.ChannelID, event.ThreadTS)
			return &ParseResult{Event: event}, nil
		}
		return &ParseResult{}, nil
	default:
		return &ParseResult{}, nil
	}
}

func scopeKey(channelID, threadTS string) string {
	if channelID == "" {
		return threadTS
	}
	if threadTS == "" {
		return channelID
	}
	return fmt.Sprintf("%s:%s", channelID, threadTS)
}
