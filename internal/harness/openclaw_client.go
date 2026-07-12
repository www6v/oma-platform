package harness

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"strings"
	"time"
)

// OpenClawClient calls an OpenClaw Gateway's OpenAI-compatible HTTP API
// (POST /v1/chat/completions). The Gateway maintains conversation state
// server-side keyed by session, so RunTurn only sends the latest user
// message plus optional system prompt per turn.
//
// Implements both Client (RunTurn) and StreamingClient (RunTurnStream).
type OpenClawClient struct {
	// GatewayURL is the base URL of the OpenClaw Gateway, e.g.
	// "http://124.221.28.203:17772". No trailing slash.
	GatewayURL string
	// Token is the Bearer token for Gateway auth.
	Token string
	// Agent is the OpenClaw model id, e.g. "openclaw/default".
	// Falls back to "openclaw/default" when empty.
	Agent string
	// HTTP is the optional *http.Client override. When nil a default
	// client with a 5-minute timeout is used.
	HTTP *http.Client
}

// openClawChatRequest is the OpenAI ChatCompletion request body.
type openClawChatRequest struct {
	Model    string              `json:"model"`
	Messages []openClawMessage   `json:"messages"`
	Stream   bool                `json:"stream,omitempty"`
	MaxTokens int               `json:"max_tokens,omitempty"`
}

type openClawMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// openClawChatResponse is the non-streaming ChatCompletion response.
type openClawChatResponse struct {
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

// openClawSSEChunk is one SSE data line from a streaming response.
type openClawSSEChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// RunTurn implements Client. Sends the last user message to OpenClaw and
// returns the response as an agent.message event.
func (c *OpenClawClient) RunTurn(
	ctx context.Context,
	req TurnRequest,
) (TurnResponse, error) {
	userText := extractLastUserMessage(req.Events)
	if userText == "" {
		userText = "(continue)"
	}

	messages := buildMessages(req.Agent.SystemPrompt, userText)
	model := c.Agent
	if model == "" {
		model = "openclaw/default"
	}

	body, err := json.Marshal(openClawChatRequest{
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
	c.setHeaders(httpReq, req.SessionID)

	client := c.httpClient()
	resp, err := client.Do(httpReq)
	if err != nil {
		return TurnResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return TurnResponse{}, fmt.Errorf(
			"openclaw status=%d: %s",
			resp.StatusCode,
			strings.TrimSpace(string(raw)),
		)
	}

	var chatResp openClawChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return TurnResponse{}, fmt.Errorf("openclaw decode: %w", err)
	}
	if chatResp.Error != nil {
		return TurnResponse{}, fmt.Errorf(
			"openclaw: %s: %s",
			chatResp.Error.Type,
			chatResp.Error.Message,
		)
	}
	if len(chatResp.Choices) == 0 {
		return TurnResponse{}, fmt.Errorf("openclaw: empty response (no choices)")
	}

	text := chatResp.Choices[0].Message.Content
	ev, err := agentMessageEvent(randomOCID(), text)
	if err != nil {
		return TurnResponse{}, err
	}
	return TurnResponse{Events: []json.RawMessage{ev}}, nil
}

// RunTurnStream implements StreamingClient. Sends the last user message
// with stream=true and emits incremental agent.message events as SSE
// chunks arrive. A final agent.message with the full accumulated text
// is emitted when the stream ends.
func (c *OpenClawClient) RunTurnStream(
	ctx context.Context,
	req TurnRequest,
	onEvent EventHandler,
) error {
	userText := extractLastUserMessage(req.Events)
	if userText == "" {
		userText = "(continue)"
	}

	messages := buildMessages(req.Agent.SystemPrompt, userText)
	model := c.Agent
	if model == "" {
		model = "openclaw/default"
	}

	body, err := json.Marshal(openClawChatRequest{
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
	c.setHeaders(httpReq, req.SessionID)

	client := c.streamingHTTPClient()
	resp, err := client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf(
			"openclaw stream status=%d: %s",
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

		var chunk openClawSSEChunk
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

		// Emit an incremental event for each content chunk.
		ev, evErr := agentMessageEvent(chunkID, accumulated.String())
		if evErr != nil {
			return evErr
		}
		if err := onEvent(ev); err != nil {
			return err
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("openclaw stream read: %w", err)
	}

	// If no content came through, emit an empty event so the caller
	// always sees at least one agent.message.
	if !emitChunk {
		ev, err := agentMessageEvent(chunkID, "")
		if err != nil {
			return err
		}
		return onEvent(ev)
	}
	return nil
}

// setHeaders applies auth, content-type and session routing headers.
func (c *OpenClawClient) setHeaders(req *http.Request, sessionID string) {
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if sessionID != "" {
		req.Header.Set("x-openclaw-session-key", "oma-"+sessionID)
	}
}

func (c *OpenClawClient) httpClient() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 5 * time.Minute}
}

// streamingHTTPClient returns an http.Client without timeout for SSE.
func (c *OpenClawClient) streamingHTTPClient() *http.Client {
	if c.HTTP != nil && c.HTTP.Timeout == 0 {
		return c.HTTP
	}
	base := c.HTTP
	if base == nil {
		base = http.DefaultClient
	}
	return &http.Client{Transport: base.Transport, Timeout: 0}
}

// buildMessages constructs the OpenClaw message list from system prompt
// and user text.
func buildMessages(systemPrompt, userText string) []openClawMessage {
	msgs := make([]openClawMessage, 0, 2)
	if systemPrompt != "" {
		msgs = append(msgs, openClawMessage{
			Role:    "system",
			Content: systemPrompt,
		})
	}
	msgs = append(msgs, openClawMessage{
		Role:    "user",
		Content: userText,
	})
	return msgs
}

// extractLastUserMessage scans events backwards for the most recent
// user.message and returns its text content. Returns "" when no user
// message is found.
func extractLastUserMessage(events []json.RawMessage) string {
	type contentBlock struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	type event struct {
		Type    string         `json:"type"`
		Content []contentBlock `json:"content"`
	}

	for i := len(events) - 1; i >= 0; i-- {
		var ev event
		if err := json.Unmarshal(events[i], &ev); err != nil {
			continue
		}
		if ev.Type != "user.message" {
			continue
		}
		for _, block := range ev.Content {
			if block.Type == "text" && block.Text != "" {
				return block.Text
			}
		}
	}
	return ""
}

// agentMessageEvent builds an oma-format agent.message event.
func agentMessageEvent(id, text string) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"type": "agent.message",
		"id":   id,
		"content": []map[string]string{
			{"type": "text", "text": text},
		},
	})
}

const ocIDAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// randomOCID generates a 12-char random id for agent.message events.
func randomOCID() string {
	out := make([]byte, 12)
	max := big.NewInt(int64(len(ocIDAlphabet)))
	for i := range out {
		idx, err := rand.Int(rand.Reader, max)
		if err != nil {
			// crypto/rand.Read only fails on broken PRNG — degrade
			// gracefully rather than panic.
			out[i] = ocIDAlphabet[i%len(ocIDAlphabet)]
			continue
		}
		out[i] = ocIDAlphabet[idx.Int64()]
	}
	return "oc_" + string(out)
}
