package modelslist

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

// Model is a provider model id + display name for Console pickers.
type Model struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Client fetches model lists from official provider APIs.
type Client struct {
	HTTP             *http.Client
	AnthropicBaseURL string
	OpenAIBaseURL    string
}

// DefaultClient is used by the platform API when no override is supplied.
var DefaultClient = &Client{HTTP: http.DefaultClient}

func (c *Client) httpClient() *http.Client {
	if c == nil || c.HTTP == nil {
		return http.DefaultClient
	}
	return c.HTTP
}

func (c *Client) anthropicBase() string {
	if c != nil && c.AnthropicBaseURL != "" {
		return strings.TrimRight(c.AnthropicBaseURL, "/")
	}
	return "https://api.anthropic.com"
}

func (c *Client) openaiBase() string {
	if c != nil && c.OpenAIBaseURL != "" {
		return strings.TrimRight(c.OpenAIBaseURL, "/")
	}
	return "https://api.openai.com"
}

// Fetch returns models for ant/oai providers, or empty for unknown providers.
func (c *Client) Fetch(
	ctx context.Context,
	provider, apiKey string,
) ([]Model, error) {
	switch provider {
	case "ant":
		return c.fetchAnthropic(ctx, apiKey)
	case "oai":
		return c.fetchOpenAI(ctx, apiKey)
	default:
		return []Model{}, nil
	}
}

func (c *Client) fetchAnthropic(
	ctx context.Context,
	apiKey string,
) ([]Model, error) {
	url := c.anthropicBase() + "/v1/models?limit=100"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Anthropic API %d", resp.StatusCode)
	}

	var parsed struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}

	out := make([]Model, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		name := item.DisplayName
		if name == "" {
			name = item.ID
		}
		out = append(out, Model{ID: item.ID, Name: name})
	}
	return out, nil
}

func (c *Client) fetchOpenAI(ctx context.Context, apiKey string) ([]Model, error) {
	url := c.openaiBase() + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("OpenAI API %d", resp.StatusCode)
	}

	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, err
	}

	prefixes := []string{"gpt-", "o1", "o3", "o4", "chatgpt-"}
	out := make([]Model, 0, len(parsed.Data))
	for _, item := range parsed.Data {
		if !hasChatPrefix(item.ID, prefixes) {
			continue
		}
		out = append(out, Model{ID: item.ID, Name: item.ID})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func hasChatPrefix(id string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(id, prefix) {
			return true
		}
	}
	return false
}
