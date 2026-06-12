package harness

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
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
	AuxModel     string          `json:"aux_model,omitempty"`
	SystemPrompt string          `json:"system_prompt,omitempty"`
	Description  string          `json:"description,omitempty"`
	Tools        json.RawMessage `json:"tools,omitempty"`
	Version      int             `json:"version"`
}

// TurnRequest is the harness turn payload.
type TurnRequest struct {
	SessionID   string            `json:"session_id"`
	Agent       AgentSnapshot     `json:"agent"`
	Model       ModelConfig       `json:"model,omitempty"`
	AuxModel    *ModelConfig      `json:"aux_model,omitempty"`
	Environment json.RawMessage   `json:"environment,omitempty"`
	Events      []json.RawMessage `json:"events"`
	Workdir     string            `json:"workdir"`
}

// TurnResponse is the harness turn result.
type TurnResponse struct {
	Events []json.RawMessage `json:"events"`
}

// EventHandler receives one harness event as it is produced.
type EventHandler func(event json.RawMessage) error

// StreamingClient streams harness events during a turn.
type StreamingClient interface {
	RunTurnStream(
		ctx context.Context,
		req TurnRequest,
		onEvent EventHandler,
	) error
}

// Client runs harness turns over HTTP.
type Client interface {
	RunTurn(ctx context.Context, req TurnRequest) (TurnResponse, error)
}

// RunTurnStreaming invokes RunTurnStream when supported, otherwise batches
// events from RunTurn.
func RunTurnStreaming(
	ctx context.Context,
	client Client,
	req TurnRequest,
	onEvent EventHandler,
) error {
	if sc, ok := client.(StreamingClient); ok {
		return sc.RunTurnStream(ctx, req, onEvent)
	}
	resp, err := client.RunTurn(ctx, req)
	if err != nil {
		return err
	}
	for _, ev := range resp.Events {
		if err := onEvent(ev); err != nil {
			return err
		}
	}
	return nil
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
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			return TurnResponse{}, fmt.Errorf(
				"harness status=%d",
				resp.StatusCode,
			)
		}
		return TurnResponse{}, fmt.Errorf(
			"harness status=%d: %s",
			resp.StatusCode,
			msg,
		)
	}
	var out TurnResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return TurnResponse{}, err
	}
	return out, nil
}

// RunTurnStream reads NDJSON lines from POST /internal/turn/stream.
func (c *HTTPClient) RunTurnStream(
	ctx context.Context,
	req TurnRequest,
	onEvent EventHandler,
) error {
	body, err := json.Marshal(req)
	if err != nil {
		return err
	}
	url := c.BaseURL + "/internal/turn/stream"
	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, url, bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/x-ndjson")

	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			return fmt.Errorf("harness stream status=%d", resp.StatusCode)
		}
		return fmt.Errorf("harness stream status=%d: %s", resp.StatusCode, msg)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if err := onEvent(json.RawMessage(line)); err != nil {
			return err
		}
	}
	return scanner.Err()
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

// RunTurnStream implements StreamingClient for tests.
func (f *FakeClient) RunTurnStream(
	ctx context.Context,
	req TurnRequest,
	onEvent EventHandler,
) error {
	resp, err := f.RunTurn(ctx, req)
	if err != nil {
		return err
	}
	for _, ev := range resp.Events {
		if err := onEvent(ev); err != nil {
			return err
		}
	}
	return nil
}

// RecordingClient captures turn requests for integration tests.
type RecordingClient struct {
	FakeClient FakeClient

	mu       sync.Mutex
	requests []TurnRequest
}

// RunTurn records the request then delegates to FakeClient.
func (r *RecordingClient) RunTurn(
	ctx context.Context,
	req TurnRequest,
) (TurnResponse, error) {
	r.mu.Lock()
	r.requests = append(r.requests, req)
	r.mu.Unlock()
	return r.FakeClient.RunTurn(ctx, req)
}

// RunTurnStream records the request then delegates to FakeClient.
func (r *RecordingClient) RunTurnStream(
	ctx context.Context,
	req TurnRequest,
	onEvent EventHandler,
) error {
	r.mu.Lock()
	r.requests = append(r.requests, req)
	r.mu.Unlock()
	return r.FakeClient.RunTurnStream(ctx, req, onEvent)
}

// LastRequest returns the most recent turn request, if any.
func (r *RecordingClient) LastRequest() (TurnRequest, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.requests) == 0 {
		return TurnRequest{}, false
	}
	return r.requests[len(r.requests)-1], true
}

// RequestCount returns how many turns were recorded.
func (r *RecordingClient) RequestCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.requests)
}
