package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// ModelConfig is the resolved provider credentials for a turn.
type ModelConfig struct {
	Model         string          `json:"model"`
	Provider      string          `json:"provider,omitempty"`
	APIKey        string          `json:"api_key,omitempty"`
	BaseURL       string          `json:"base_url,omitempty"`
	CustomHeaders json.RawMessage `json:"custom_headers,omitempty"`
}

// AgentSnapshot is the agent config sent to the harness sidecar.
type AgentSnapshot struct {
	ID           string          `json:"id"`
	Name         string          `json:"name"`
	Model        string          `json:"model"`
	SystemPrompt string          `json:"system_prompt,omitempty"`
	Description  string          `json:"description,omitempty"`
	Tools        json.RawMessage `json:"tools,omitempty"`
	Version      int             `json:"version"`
}

// TurnRequest is the harness turn payload.
type TurnRequest struct {
	SessionID string            `json:"session_id"`
	Agent     AgentSnapshot     `json:"agent"`
	Model     ModelConfig       `json:"model,omitempty"`
	Events    []json.RawMessage `json:"events"`
	Workdir   string            `json:"workdir"`
}

// TurnResponse is the harness turn result.
type TurnResponse struct {
	Events []json.RawMessage `json:"events"`
}

// Client runs harness turns over HTTP.
type Client interface {
	RunTurn(ctx context.Context, req TurnRequest) (TurnResponse, error)
}

// HTTPClient calls a FastAPI harness sidecar.
type HTTPClient struct {
	BaseURL string
	HTTP    *http.Client
}

// RunTurn posts to POST /internal/turn.
func (c *HTTPClient) RunTurn(
	ctx context.Context,
	req TurnRequest,
) (TurnResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return TurnResponse{}, err
	}
	url := c.BaseURL + "/internal/turn"
	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, url, bytes.NewReader(body),
	)
	if err != nil {
		return TurnResponse{}, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return TurnResponse{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return TurnResponse{}, fmt.Errorf("harness status=%d", resp.StatusCode)
	}
	var out TurnResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return TurnResponse{}, err
	}
	return out, nil
}

// FakeClient emits a single agent.message for tests.
type FakeClient struct {
	Text string
}

// RunTurn implements Client.
func (f *FakeClient) RunTurn(
	_ context.Context,
	_ TurnRequest,
) (TurnResponse, error) {
	text := f.Text
	if text == "" {
		text = "ok"
	}
	payload, _ := json.Marshal(map[string]any{
		"type": "agent.message",
		"id":   "evt_fake",
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	})
	return TurnResponse{Events: []json.RawMessage{payload}}, nil
}
