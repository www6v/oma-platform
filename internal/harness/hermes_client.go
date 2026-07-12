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
	"time"
)

// HermesClient calls a Hermes Agent's OpenAI-compatible HTTP API
// (POST /v1/chat/completions). Unlike OpenClaw, Hermes is stateless — the
// full conversation history must be sent in the messages array each turn.
// See https://hermes-doc.aigc.green/user-guide/features/api-server.
//
// Implements both Client (RunTurn) and StreamingClient (RunTurnStream).
type HermesClient struct {
	// GatewayURL is the base URL of the Hermes API server, e.g.
	// "http://124.221.28.203:8642". No trailing slash.
	GatewayURL string
	// Token is the Bearer token (API_SERVER_KEY) for auth.
	Token string
	// Model is the Hermes model id. Defaults to "hermes-agent" when empty.
	Model string
	// HTTP is the optional *http.Client override. When nil a default
	// client with a 10-minute timeout is used (Hermes agents may do
	// multi-step tool calls that take a while).
	HTTP *http.Client
}

// hermesChatRequest is the OpenAI ChatCompletion request body.
type hermesChatRequest struct {
	Model     string            `json:"model"`
	Messages  []hermesMessage   `json:"messages"`
	Stream    bool              `json:"stream,omitempty"`
	MaxTokens int               `json:"max_tokens,omitempty"`
}

type hermesMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// hermesChatResponse is the non-streaming ChatCompletion response.
type hermesChatResponse struct {
	Choices []struct {
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// hermesSSEChunk is one SSE data line from a streaming response.
type hermesSSEChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// RunTurn implements Client. Converts the full event history to OpenAI
// messages, POSTs to /v1/chat/completions, and returns the response as an
// agent.message event.
func (c *HermesClient) RunTurn(
	ctx context.Context,
	req TurnRequest,
) (TurnResponse, error) {
	messages := c.buildMessages(req)
	model := c.Model
	if model == "" {
		model = "hermes-agent"
	}

	body, err := json.Marshal(hermesChatRequest{
		Model:    model,
		Messages: messages,
	})
	if err != nil {
		return TurnResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost,
		c.GatewayURL+"/v1/chat/completions",
		bytes.NewReader(body),
	)
	if err != nil {
		return TurnResponse{}, err
	}
	c.setHeaders(httpReq)

	client := c.httpClient()
	resp, err := client.Do(httpReq)
	if err != nil {
		return TurnResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return TurnResponse{}, fmt.Errorf(
			"hermes status=%d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(raw)),
		)
	}

	var chatResp hermesChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return TurnResponse{}, fmt.Errorf("hermes decode: %w", err)
	}
	if chatResp.Error != nil {
		return TurnResponse{}, fmt.Errorf(
			"hermes: %s: %s",
			chatResp.Error.Type,
			chatResp.Error.Message,
		)
	}
	if len(chatResp.Choices) == 0 {
		return TurnResponse{}, fmt.Errorf("hermes: empty response (no choices)")
	}

	text := chatResp.Choices[0].Message.Content
	ev, err := agentMessageEvent(randomOCID(), text)
	if err != nil {
		return TurnResponse{}, err
	}
	return TurnResponse{Events: []json.RawMessage{ev}}, nil
}

// RunTurnStream implements StreamingClient. Same as RunTurn but with
// stream=true, emitting incremental agent.message events as SSE chunks
// arrive.
func (c *HermesClient) RunTurnStream(
	ctx context.Context,
	req TurnRequest,
	onEvent EventHandler,
) error {
	messages := c.buildMessages(req)
	model := c.Model
	if model == "" {
		model = "hermes-agent"
	}

	body, err := json.Marshal(hermesChatRequest{
		Model:    model,
		Messages: messages,
		Stream:   true,
	})
	if err != nil {
		return err
	}

	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost,
		c.GatewayURL+"/v1/chat/completions",
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	c.setHeaders(httpReq)

	client := c.streamingHTTPClient()
	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf(
			"hermes stream status=%d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(raw)),
		)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)

	var accumulated strings.Builder
	chunkID := randomOCID()
	emitChunk := false

	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if !bytes.HasPrefix(line, []byte("data: ")) {
			continue
		}
		data := bytes.TrimSpace(line[6:])
		if string(data) == "[DONE]" {
			break
		}

		var chunk hermesSSEChunk
		if err := json.Unmarshal(data, &chunk); err != nil {
			continue
		}
		if len(chunk.Choices) == 0 {
			continue
		}

		delta := chunk.Choices[0].Delta.Content
		if delta == "" {
			continue
		}
		accumulated.WriteString(delta)
		emitChunk = true

		ev, evErr := agentMessageEvent(chunkID, accumulated.String())
		if evErr != nil {
			return evErr
		}
		if err := onEvent(ev); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("hermes stream read: %w", err)
	}

	if !emitChunk {
		ev, err := agentMessageEvent(chunkID, "")
		if err != nil {
			return err
		}
		return onEvent(ev)
	}
	return nil
}

// setHeaders applies auth and content-type headers.
func (c *HermesClient) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
}

func (c *HermesClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	// Hermes agents may do multi-step tool calls (terminal, web search,
	// etc.) — allow up to 10 minutes.
	return &http.Client{Timeout: 10 * time.Minute}
}

// streamingHTTPClient returns an http.Client without timeout for SSE.
func (c *HermesClient) streamingHTTPClient() *http.Client {
	if c.HTTP != nil && c.HTTP.Timeout == 0 {
		return c.HTTP
	}
	base := c.HTTP
	if base == nil {
		base = http.DefaultClient
	}
	return &http.Client{Transport: base.Transport, Timeout: 0}
}

// buildMessages converts the full event history + system prompt to OpenAI
// messages. Unlike OpenClaw (which maintains server-side session state),
// Hermes is stateless — we must replay the full conversation each turn.
func (c *HermesClient) buildMessages(req TurnRequest) []hermesMessage {
	msgs := make([]hermesMessage, 0, len(req.Events)+1)
	if req.Agent.SystemPrompt != "" {
		msgs = append(msgs, hermesMessage{
			Role:    "system",
			Content: req.Agent.SystemPrompt,
		})
	}
	msgs = append(msgs, eventsToHermesMessages(req.Events)...)
	// Guarantee at least one user message — Hermes requires it.
	if !hasRole(msgs, "user") {
		msgs = append(msgs, hermesMessage{
			Role:    "user",
			Content: "(continue)",
		})
	}
	return msgs
}

// eventsToHermesMessages converts an oma event list to OpenAI messages.
// Recognised event types:
//
//	user.message   → role=user (text blocks concatenated)
//	agent.message  → role=assistant (text blocks concatenated)
//
// Other event types (lifecycle, tool_use, etc.) are skipped — Hermes
// doesn't consume them via chat completions.
func eventsToHermesMessages(events []json.RawMessage) []hermesMessage {
	type contentBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type event struct {
		Type    string         `json:"type"`
		Content []contentBlock `json:"content"`
	}

	var msgs []hermesMessage
	for _, raw := range events {
		var ev event
		if err := json.Unmarshal(raw, &ev); err != nil {
			continue
		}
		var role string
		switch ev.Type {
		case "user.message":
			role = "user"
		case "agent.message":
			role = "assistant"
		default:
			continue
		}
		var text strings.Builder
		for _, block := range ev.Content {
			if block.Type == "text" {
				text.WriteString(block.Text)
			}
		}
		if text.Len() == 0 {
			continue
		}
		msgs = append(msgs, hermesMessage{
			Role:    role,
			Content: text.String(),
		})
	}
	return msgs
}

// hasRole reports whether msgs contains at least one message with the
// given role.
func hasRole(msgs []hermesMessage, role string) bool {
	for _, m := range msgs {
		if m.Role == role {
			return true
		}
	}
	return false
}
